package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type Fetcher interface {
	FetchObject(context.Context, string, *actors.Actor) (map[string]any, error)
}

type WebFinger interface {
	ResolveActor(context.Context, string) (string, error)
}

type Resolver struct {
	repo      actors.Repository
	fetcher   Fetcher
	signer    *actors.Actor
	webFinger WebFinger
}

func New(repo actors.Repository, fetcher Fetcher, signer *actors.Actor) *Resolver {
	return &Resolver{repo: repo, fetcher: fetcher, signer: signer}
}

func NewWithWebFinger(repo actors.Repository, fetcher Fetcher, signer *actors.Actor, webFinger WebFinger) *Resolver {
	return &Resolver{repo: repo, fetcher: fetcher, signer: signer, webFinger: webFinger}
}

func (r *Resolver) ResolveActorHandle(ctx context.Context, handle string) (*actors.Actor, error) {
	if r.webFinger == nil {
		return nil, fmt.Errorf("webfinger resolver is not configured")
	}
	uri, err := r.webFinger.ResolveActor(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("resolve actor handle: %w", err)
	}
	return r.ResolveActor(ctx, uri)
}

func (r *Resolver) ResolveActor(ctx context.Context, uri string) (*actors.Actor, error) {
	if uri == "" {
		return nil, fmt.Errorf("actor uri is required")
	}
	existing, err := r.repo.FindByURI(ctx, uri)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	object, err := r.fetcher.FetchObject(ctx, uri, r.signer)
	if err != nil {
		return nil, err
	}
	actor, err := ParseRemoteActor(object, uri)
	if err != nil {
		return nil, err
	}
	return r.repo.UpsertRemoteActor(ctx, actor)
}

func ParseRemoteActor(object map[string]any, uri string) (actors.Actor, error) {
	if object == nil {
		return actors.Actor{}, fmt.Errorf("invalid actor: object is null")
	}
	if !aptypes.IsActor(object) {
		return actors.Actor{}, fmt.Errorf("invalid actor type")
	}
	id, err := aptypes.GetAPID(object)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("invalid actor id: %w", err)
	}
	expectHost, err := hostOf(uri)
	if err != nil {
		return actors.Actor{}, err
	}
	idHost, err := hostOf(id)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("invalid actor id host: %w", err)
	}
	if idHost != expectHost {
		return actors.Actor{}, fmt.Errorf("invalid actor: id has different host")
	}
	inbox, err := requiredSameHostID(object["inbox"], expectHost, "inbox")
	if err != nil {
		return actors.Actor{}, err
	}
	if _, err := requiredSameHostID(object["outbox"], expectHost, "outbox"); err != nil {
		return actors.Actor{}, err
	}
	sharedInbox, err := optionalSharedInbox(object, expectHost)
	if err != nil {
		return actors.Actor{}, err
	}
	followersURI, err := optionalSameHostID(object["followers"], expectHost, "followers")
	if err != nil {
		return actors.Actor{}, err
	}
	followingURI, err := optionalSameHostID(object["following"], expectHost, "following")
	if err != nil {
		return actors.Actor{}, err
	}
	featuredURI := optionalAPID(object["featured"])
	username, ok := object["preferredUsername"].(string)
	if !ok || !validRemoteUsername(username) {
		return actors.Actor{}, fmt.Errorf("invalid actor: wrong username")
	}
	name, _ := object["name"].(string)
	if len(name) > 128 {
		name = name[:128]
	}
	publicKeyID, publicKeyPEM, err := publicKey(object, expectHost)
	if err != nil {
		return actors.Actor{}, err
	}
	host := expectHost
	return actors.Actor{
		ID:            remoteActorID(id),
		Username:      username,
		UsernameLower: strings.ToLower(username),
		Name:          name,
		Type:          actorType(object),
		Host:          &host,
		URI:           id,
		Inbox:         inbox,
		SharedInbox:   sharedInbox,
		FollowersURI:  followersURI,
		FollowingURI:  followingURI,
		FeaturedURI:   featuredURI,
		PublicKeyID:   publicKeyID,
		PublicKeyPEM:  publicKeyPEM,
		IsSuspended:   false,
	}, nil
}

func requiredSameHostID(value any, expectHost, field string) (string, error) {
	id, err := aptypes.GetAPID(value)
	if err != nil {
		return "", fmt.Errorf("invalid actor: wrong %s", field)
	}
	host, err := hostOf(id)
	if err != nil || host != expectHost {
		return "", fmt.Errorf("invalid actor: wrong %s", field)
	}
	return id, nil
}

func optionalSharedInbox(object map[string]any, expectHost string) (string, error) {
	value := object["sharedInbox"]
	if value == nil {
		if endpoints, ok := object["endpoints"].(map[string]any); ok {
			value = endpoints["sharedInbox"]
		}
	}
	if value == nil {
		return "", nil
	}
	return requiredSameHostID(value, expectHost, "shared inbox")
}

func optionalSameHostID(value any, expectHost, field string) (string, error) {
	if value == nil {
		return "", nil
	}
	return requiredSameHostID(value, expectHost, field)
}

func optionalAPID(value any) string {
	if value == nil {
		return ""
	}
	id, err := aptypes.GetAPID(value)
	if err != nil {
		return ""
	}
	return id
}

func publicKey(object map[string]any, expectHost string) (string, string, error) {
	value, ok := object["publicKey"].(map[string]any)
	if !ok || value == nil {
		return "", "", nil
	}
	id, ok := value["id"].(string)
	if !ok || id == "" {
		return "", "", fmt.Errorf("invalid actor: publicKey.id is not a string")
	}
	host, err := hostOf(id)
	if err != nil || host != expectHost {
		return "", "", fmt.Errorf("invalid actor: publicKey.id has different host")
	}
	pem, ok := value["publicKeyPem"].(string)
	if !ok || pem == "" {
		return "", "", fmt.Errorf("invalid actor: publicKey.publicKeyPem is not a string")
	}
	return id, pem, nil
}

func actorType(object map[string]any) string {
	typ, err := aptypes.GetAPType(object)
	if err != nil {
		return "Person"
	}
	return typ
}

func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid url: %s", raw)
	}
	return strings.ToLower(u.Hostname()), nil
}

func remoteActorID(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	return "remote_" + hex.EncodeToString(sum[:])[:24]
}

func validRemoteUsername(username string) bool {
	if username == "" || len(username) > 128 {
		return false
	}
	for i, r := range username {
		ok := r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !ok {
			return false
		}
		if (i == 0 || i == len(username)-1) && (r == '-' || r == '.') {
			return false
		}
	}
	return true
}

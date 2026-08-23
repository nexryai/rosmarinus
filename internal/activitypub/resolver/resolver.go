package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/mfm"
)

type Fetcher interface {
	FetchObject(context.Context, string, *actors.Actor) (map[string]any, error)
}

type WebFinger interface {
	ResolveActor(context.Context, string) (string, error)
}

type FederationPolicy interface {
	IsFederationHostBlocked(string) bool
	IsSelfFederationURL(string) bool
}

type Resolver struct {
	repo      actors.Repository
	fetcher   Fetcher
	signer    *actors.Actor
	webFinger WebFinger
	policy    FederationPolicy
}

const remoteActorRefreshInterval = 24 * time.Hour

const (
	maxActorSummaryLength  = 2048
	maxActorLocationLength = 128
	maxActorURLLength      = 512
)

var actorBirthdayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func (r *Resolver) SetFederationPolicy(policy FederationPolicy) {
	r.policy = policy
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
	host, err := resolvableHostOf(uri)
	if err != nil {
		return nil, err
	}
	if r.policy != nil && r.policy.IsFederationHostBlocked(host) {
		return nil, fmt.Errorf("actor host is blocked: %s", host)
	}
	existing, err := r.repo.FindByURI(ctx, uri)
	if err != nil {
		return nil, err
	}
	if existing != nil && (existing.Host == nil || (!existing.LastFetchedAt.IsZero() && time.Since(existing.LastFetchedAt) < remoteActorRefreshInterval)) {
		return existing, nil
	}
	if r.policy != nil && r.policy.IsSelfFederationURL(uri) {
		return nil, fmt.Errorf("local actor not found: %s", uri)
	}
	if r.fetcher == nil {
		if existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("actor fetcher is not configured")
	}
	object, err := r.fetcher.FetchObject(ctx, uri, r.signer)
	if err != nil {
		if existing != nil {
			return existing, nil
		}
		return nil, err
	}
	actor, err := ParseRemoteActor(object, uri)
	if err != nil {
		if existing != nil {
			return existing, nil
		}
		return nil, err
	}
	return r.repo.UpsertRemoteActor(ctx, actor)
}

func resolvableHostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("invalid resolvable url: %s", raw)
	}
	return strings.ToLower(u.Hostname()), nil
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
	if _, err := optionalSameHostID(object["outbox"], expectHost, "outbox"); err != nil {
		return actors.Actor{}, err
	}
	sharedInbox := optionalSharedInbox(object, expectHost)
	followersURI, err := optionalSameHostID(object["followers"], expectHost, "followers")
	if err != nil {
		return actors.Actor{}, err
	}
	followingURI, err := optionalSameHostID(object["following"], expectHost, "following")
	if err != nil {
		return actors.Actor{}, err
	}
	featuredURI := optionalAPID(object["featured"])
	movedToURI := optionalAPID(object["movedTo"])
	alsoKnownAs := aptypes.GetAPIDs(object["alsoKnownAs"])
	username, ok := object["preferredUsername"].(string)
	if !ok || !validRemoteUsername(username) {
		return actors.Actor{}, fmt.Errorf("invalid actor: wrong username")
	}
	name, _ := object["name"].(string)
	name = truncateRunes(name, 128)
	summary, err := actorSummary(object)
	if err != nil {
		return actors.Actor{}, err
	}
	profileURL, err := optionalHTTPSHref(object["url"], maxActorURLLength)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("invalid actor url: %w", err)
	}
	avatarURL, _ := optionalHTTPSHref(object["icon"], maxActorURLLength)
	bannerURL, _ := optionalHTTPSHref(object["image"], maxActorURLLength)
	profileFields := actorProfileFields(object["attachment"])
	tags := actorHashtags(object["tag"])
	emojiNames := actorEmojiNames(object["tag"])
	birthday := actorBirthday(object["vcard:bday"])
	location, _ := object["vcard:Address"].(string)
	location = truncateRunes(location, maxActorLocationLength)
	publicKeyID, publicKeyPEM, err := publicKey(object, expectHost)
	if err != nil {
		return actors.Actor{}, err
	}
	kind := actorType(object)
	host := expectHost
	return actors.Actor{
		ID:             remoteActorID(id),
		Username:       username,
		UsernameLower:  strings.ToLower(username),
		Name:           name,
		Summary:        summary,
		URL:            profileURL,
		ProfileFields:  profileFields,
		Birthday:       birthday,
		Location:       location,
		AvatarURL:      avatarURL,
		BannerURL:      bannerURL,
		Tags:           tags,
		EmojiNames:     emojiNames,
		IsBot:          kind == "Service" || kind == "Application",
		IsCat:          boolValue(object["isCat"]),
		IsLocked:       boolValue(object["manuallyApprovesFollowers"]),
		IsDiscoverable: boolValue(object["discoverable"]),
		Type:           kind,
		Host:           &host,
		URI:            id,
		Inbox:          inbox,
		SharedInbox:    sharedInbox,
		FollowersURI:   followersURI,
		FollowingURI:   followingURI,
		FeaturedURI:    featuredURI,
		MovedToURI:     movedToURI,
		AlsoKnownAs:    alsoKnownAs,
		PublicKeyID:    publicKeyID,
		PublicKeyPEM:   publicKeyPEM,
		IsSuspended:    false,
	}, nil
}

func actorSummary(object map[string]any) (string, error) {
	if summary, ok := object["_misskey_summary"].(string); ok && summary != "" {
		return truncateRunes(summary, maxActorSummaryLength), nil
	}
	value, exists := object["summary"]
	if !exists || value == nil || value == "" {
		return "", nil
	}
	summary, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid actor: wrong summary")
	}
	hashtags := actorHashtags(object["tag"])
	for i := range hashtags {
		hashtags[i] = "#" + hashtags[i]
	}
	converted, err := mfm.FromHTML(truncateRunes(summary, maxActorSummaryLength), hashtags)
	if err != nil {
		return "", fmt.Errorf("invalid actor summary: %w", err)
	}
	return converted, nil
}

func actorProfileFields(value any) []actors.ProfileField {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	fields := make([]actors.ProfileField, 0, len(items))
	for _, item := range items {
		property, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(property, "PropertyValue") {
			continue
		}
		name, nameOK := property["name"].(string)
		value, valueOK := property["value"].(string)
		if !nameOK || !valueOK {
			continue
		}
		converted, err := mfm.FromHTML(value, nil)
		if err != nil {
			continue
		}
		fields = append(fields, actors.ProfileField{Name: truncateRunes(name, 128), Value: truncateRunes(converted, 2048)})
	}
	return fields
}

func actorHashtags(value any) []string {
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range aptypes.ToArray(value) {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Hashtag") {
			continue
		}
		name, _ := tag["name"].(string)
		name = strings.TrimPrefix(strings.TrimSpace(name), "#")
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
		if len(result) == 32 {
			break
		}
	}
	return result
}

func actorEmojiNames(value any) []string {
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range aptypes.ToArray(value) {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Emoji") {
			continue
		}
		name, _ := tag["name"].(string)
		name = strings.Trim(strings.TrimSpace(name), ":")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func actorBirthday(value any) string {
	birthday, _ := value.(string)
	return actorBirthdayPattern.FindString(birthday)
}

func optionalHTTPSHref(value any, maxLength int) (string, error) {
	for _, item := range aptypes.ToArray(value) {
		var href string
		switch link := item.(type) {
		case string:
			href = link
		case map[string]any:
			href, _ = link["url"].(string)
			if href == "" {
				href, _ = link["href"].(string)
			}
		}
		if href == "" {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return "", fmt.Errorf("unexpected schema: %s", href)
		}
		if utf8.RuneCountInString(href) > maxLength {
			return "", fmt.Errorf("url exceeds %d characters", maxLength)
		}
		return href, nil
	}
	return "", nil
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func truncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
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

func optionalSharedInbox(object map[string]any, expectHost string) string {
	value := object["sharedInbox"]
	if value == nil {
		if endpoints, ok := object["endpoints"].(map[string]any); ok {
			value = endpoints["sharedInbox"]
		}
	}
	if value == nil {
		return ""
	}
	sharedInbox, err := requiredSameHostID(value, expectHost, "shared inbox")
	if err != nil {
		// A shared inbox is only an optimization. Falling back to the validated
		// individual inbox preserves federation without trusting a foreign host.
		return ""
	}
	return sharedInbox
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

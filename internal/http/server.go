package httpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/queue"
)

const inboxBodyLimit = 64 * 1024

type ActorLookup interface {
	FindByID(context.Context, string) (*actors.Actor, error)
	FindLocalByID(context.Context, string) (*actors.Actor, error)
	FindLocalByUsername(context.Context, string) (*actors.Actor, error)
}

type QueueClient interface {
	Enqueue(context.Context, queue.Task) error
}

type NoteLookup interface {
	FindByID(context.Context, string) (*domainnotes.Note, error)
}

type FollowLookup interface {
	CountFollowers(context.Context, string) (int, error)
	CountFollowing(context.Context, string) (int, error)
	ListFollowers(context.Context, string, int) ([]follows.Follow, error)
	ListFollowing(context.Context, string, int) ([]follows.Follow, error)
}

type ReactionLookup interface {
	FindByID(context.Context, string) (*reactions.Reaction, error)
}

func NewHandler(cfg config.Config, logger *log.Logger, actorLookup ActorLookup, queueClient QueueClient) http.Handler {
	return NewHandlerWithStores(cfg, logger, actorLookup, nil, nil, nil, queueClient)
}

func NewHandlerWithStores(cfg config.Config, logger *log.Logger, actorLookup ActorLookup, noteLookup NoteLookup, followLookup FollowLookup, reactionLookup ReactionLookup, queueClient QueueClient) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/inbox", inbox(cfg, queueClient))
	mux.HandleFunc("/users/", actorByID(cfg, actorLookup, followLookup, queueClient, logger))
	mux.HandleFunc("/notes/", noteByID(noteLookup))
	mux.HandleFunc("/emojis/", notImplemented(logger, http.MethodGet))
	mux.HandleFunc("/likes/", likeByID(cfg, reactionLookup, noteLookup))
	mux.HandleFunc("/follows/", followByID(cfg, actorLookup))
	mux.HandleFunc("/.well-known/", wellKnown(cfg, actorLookup))
	mux.HandleFunc("/nodeinfo/", nodeInfo(cfg))
	mux.HandleFunc("/", fallback(cfg, actorLookup, logger))
	return mux
}

func likeByID(cfg config.Config, reactionLookup ReactionLookup, noteLookup NoteLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if reactionLookup == nil || noteLookup == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/likes/"), "/")
		if id == "" || strings.Contains(id, "/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reaction, err := reactionLookup.FindByID(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if reaction == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		note, err := noteLookup.FindByID(r.Context(), reaction.NoteID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if note == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeActivityJSON(w, map[string]any{
			"@context": []any{
				"https://www.w3.org/ns/activitystreams",
				"https://w3id.org/security/v1",
			},
			"id":                strings.TrimRight(cfg.PublicURL, "/") + "/likes/" + url.PathEscape(reaction.ID),
			"type":              "Like",
			"actor":             reaction.ActorURI,
			"object":            note.URI,
			"content":           reaction.Reaction,
			"_misskey_reaction": reaction.Reaction,
		})
	}
}

func followByID(cfg config.Config, actorLookup ActorLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if actorLookup == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/follows/"), "/")
		parts := strings.Split(path, "/")
		if path == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		follower, err := actorLookup.FindByID(r.Context(), parts[0])
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		followee, err := actorLookup.FindByID(r.Context(), parts[1])
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Concorde exposes this resource while an outgoing Follow is still
		// pending, so actor identity is the authority rather than follow state.
		if follower == nil || follower.Host != nil || followee == nil || followee.Host == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		base := strings.TrimRight(cfg.PublicURL, "/")
		writeActivityJSON(w, map[string]any{
			"@context": []any{
				"https://www.w3.org/ns/activitystreams",
				"https://w3id.org/security/v1",
			},
			"id":     base + "/follows/" + url.PathEscape(follower.ID) + "/" + url.PathEscape(followee.ID),
			"type":   "Follow",
			"actor":  follower.URI,
			"object": followee.URI,
		})
	}
}

func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func notImplemented(logger *log.Logger, methods ...string) http.HandlerFunc {
	allowed := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		allowed[method] = struct{}{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Method]; !ok {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if logger != nil {
			logger.Printf("http: route skeleton hit method=%s path=%s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":"not implemented"}` + "\n"))
	}
}

func actorByID(cfg config.Config, actorLookup ActorLookup, followLookup FollowLookup, queueClient QueueClient, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/inbox") {
			inbox(cfg, queueClient)(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/users/")
		path = strings.Trim(path, "/")
		if path == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		parts := strings.Split(path, "/")
		actor, err := actorLookup.FindLocalByID(r.Context(), parts[0])
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if actor == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if len(parts) == 2 && parts[1] == "publickey" {
			writeActivityJSON(w, renderPublicKey(actor))
			return
		}
		if len(parts) == 2 {
			switch parts[1] {
			case "outbox", "followers", "following":
				body, err := renderActorCollection(r, cfg, actor, parts[1], followLookup)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				writeActivityJSON(w, body)
				return
			}
		}
		if len(parts) == 3 && parts[1] == "collections" && parts[2] == "featured" {
			writeActivityJSON(w, renderFeaturedCollection(actor))
			return
		}
		if len(parts) == 1 {
			writeActivityJSON(w, renderActor(cfg, actor))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func noteByID(noteLookup NoteLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if noteLookup == nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "note lookup is not configured"})
			return
		}
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/notes/"), "/")
		parts := strings.Split(path, "/")
		if path == "" || len(parts) > 2 || (len(parts) == 2 && parts[1] != "activity") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		note, err := noteLookup.FindByID(r.Context(), parts[0])
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if note == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if len(parts) == 2 {
			writeActivityJSON(w, apnotes.RenderCreate(note))
			return
		}
		writeActivityJSON(w, apnotes.Render(note))
	}
}

func inbox(cfg config.Config, queueClient QueueClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if queueClient == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "queue is not configured"})
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, inboxBodyLimit))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body is too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if len(raw) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty request body"})
			return
		}
		if err := apsig.VerifyDigest(r.Header.Get("Digest"), raw); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		parsedSignature, err := apsig.ParseRequest(r, []string{"(request-target)", "digest", "host", "date"})
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		if r.Host != cfg.Host {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host header"})
			return
		}
		var activity map[string]any
		if err := json.Unmarshal(raw, &activity); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		task := queue.NewInboxTask(activity, map[string]any{
			"keyId":         parsedSignature.KeyID,
			"algorithm":     parsedSignature.Algorithm,
			"headers":       parsedSignature.Headers,
			"signature":     base64.StdEncoding.EncodeToString(parsedSignature.Signature),
			"signingString": parsedSignature.SigningString,
		}, cfg.InboxQueue.MaxRetry, cfg.InboxQueue.Timeout)
		if err := queueClient.Enqueue(r.Context(), task); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

func actorByUsername(cfg config.Config, actorLookup ActorLookup, logger *log.Logger) http.HandlerFunc {
	_ = logger
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		username := strings.TrimPrefix(r.URL.Path, "/@")
		username = strings.Trim(username, "/")
		if username == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		actor, err := actorLookup.FindLocalByUsername(r.Context(), username)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if actor == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeActivityJSON(w, renderActor(cfg, actor))
	}
}

func fallback(cfg config.Config, actorLookup ActorLookup, logger *log.Logger) http.HandlerFunc {
	actorHandler := actorByUsername(cfg, actorLookup, logger)
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/@") {
			actorHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func wellKnown(cfg config.Config, actorLookup ActorLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setWellKnownCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/.well-known/host-meta":
			hostMeta(w, cfg)
		case "/.well-known/host-meta.json":
			hostMetaJSON(w, cfg)
		case "/.well-known/nodeinfo":
			writeJSON(w, http.StatusOK, map[string]any{"links": nodeInfoLinks(cfg)})
		case "/.well-known/webfinger":
			webFinger(w, r, cfg, actorLookup)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func nodeInfo(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/nodeinfo/2.0":
			writeJSON(w, http.StatusOK, nodeInfoBody(cfg, "2.0"))
		case "/nodeinfo/2.1":
			writeJSON(w, http.StatusOK, nodeInfoBody(cfg, "2.1"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func hostMeta(w http.ResponseWriter, cfg config.Config) {
	w.Header().Set("Content-Type", "application/xrd+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Link rel="lrdd" type="application/xrd+xml" template="%s/.well-known/webfinger?resource={uri}"/></XRD>`, html.EscapeString(strings.TrimRight(cfg.PublicURL, "/")))
}

func hostMetaJSON(w http.ResponseWriter, cfg config.Config) {
	writeJSON(w, http.StatusOK, map[string]any{
		"links": []map[string]string{{
			"rel":      "lrdd",
			"type":     "application/jrd+json",
			"template": strings.TrimRight(cfg.PublicURL, "/") + "/.well-known/webfinger?resource={uri}",
		}},
	})
}

func webFinger(w http.ResponseWriter, r *http.Request, cfg config.Config, actorLookup ActorLookup) {
	if r.URL.Query().Get("resource") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "resource is required"})
		return
	}
	if actorLookup == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "actor lookup is not configured"})
		return
	}
	actor, status, err := lookupWebFingerActor(r.Context(), cfg, actorLookup, r.URL.Query().Get("resource"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	if accepts(r, "application/xrd+xml") {
		webFingerXRD(w, cfg, actor)
		return
	}
	webFingerJRD(w, cfg, actor)
}

func nodeInfoLinks(cfg config.Config) []map[string]string {
	base := strings.TrimRight(cfg.PublicURL, "/")
	return []map[string]string{
		{
			"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.0",
			"href": base + "/nodeinfo/2.0",
		},
		{
			"rel":  "http://nodeinfo.diaspora.software/ns/schema/2.1",
			"href": base + "/nodeinfo/2.1",
		},
	}
}

func nodeInfoBody(cfg config.Config, version string) map[string]any {
	return map[string]any{
		"version": version,
		"software": map[string]any{
			"name":    "rosmarinus",
			"version": "0.0.1",
		},
		"protocols":         []string{"activitypub"},
		"services":          map[string]any{"inbound": []string{}, "outbound": []string{}},
		"openRegistrations": false,
		"usage": map[string]any{
			"users":         map[string]any{"total": 0, "activeHalfyear": 0, "activeMonth": 0},
			"localPosts":    0,
			"localComments": 0,
		},
		"metadata": map[string]any{
			"nodeName":        cfg.Host,
			"nodeDescription": "Rosmarinus ActivityPub server",
		},
	}
}

func setWellKnownCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Headers", "Accept")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Vary")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeActivityJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", `application/activity+json; charset=utf-8`)
	w.Header().Set("Cache-Control", "public, max-age=180")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func renderActor(cfg config.Config, actor *actors.Actor) map[string]any {
	actorType := actor.Type
	if actorType == "" {
		actorType = "Service"
	}
	actorURI := actor.URI
	if actorURI == "" {
		actorURI = strings.TrimRight(cfg.PublicURL, "/") + "/users/" + url.PathEscape(actor.ID)
	}
	inbox := actor.Inbox
	if inbox == "" {
		inbox = actorURI + "/inbox"
	}
	sharedInbox := actor.SharedInbox
	if sharedInbox == "" {
		sharedInbox = strings.TrimRight(cfg.PublicURL, "/") + "/inbox"
	}
	return withActivityContext(map[string]any{
		"type":                      actorType,
		"id":                        actorURI,
		"inbox":                     inbox,
		"outbox":                    actorURI + "/outbox",
		"followers":                 actorURI + "/followers",
		"following":                 actorURI + "/following",
		"featured":                  actorURI + "/collections/featured",
		"sharedInbox":               sharedInbox,
		"endpoints":                 map[string]any{"sharedInbox": sharedInbox},
		"url":                       strings.TrimRight(cfg.PublicURL, "/") + "/@" + url.PathEscape(actor.Username),
		"preferredUsername":         actor.Username,
		"name":                      actor.Name,
		"summary":                   nil,
		"manuallyApprovesFollowers": true,
		"discoverable":              true,
		"publicKey":                 renderPublicKey(actor),
		"alsoKnownAs":               []string{},
		"attachment":                []any{},
		"tag":                       []any{},
	})
}

func renderPublicKey(actor *actors.Actor) map[string]any {
	keyID := actor.PublicKeyID
	if keyID == "" && actor.URI != "" {
		keyID = actor.URI + "#main-key"
	}
	return withActivityContext(map[string]any{
		"id":           keyID,
		"type":         "Key",
		"owner":        actor.URI,
		"publicKeyPem": actor.PublicKeyPEM,
	})
}

func renderActorCollection(r *http.Request, cfg config.Config, actor *actors.Actor, name string, followLookup FollowLookup) (map[string]any, error) {
	partOf := strings.TrimRight(actor.URI, "/") + "/" + name
	totalItems, err := actorCollectionCount(r.Context(), actor, name, followLookup)
	if err != nil {
		return nil, err
	}
	if r.URL.Query().Get("page") == "true" {
		items, err := actorCollectionItems(r.Context(), actor, name, followLookup, 10)
		if err != nil {
			return nil, err
		}
		return renderOrderedCollectionPage(publicRequestURL(cfg, r), totalItems, items, partOf, "", ""), nil
	}

	var first string
	var last string
	switch name {
	case "outbox":
		first = partOf + "?page=true"
		last = partOf + "?page=true&since_id=000000000000000000000000"
	case "followers", "following":
		first = partOf + "?page=true"
	}
	return renderOrderedCollection(partOf, totalItems, first, last, nil), nil
}

func renderFeaturedCollection(actor *actors.Actor) map[string]any {
	id := strings.TrimRight(actor.URI, "/") + "/collections/featured"
	return renderOrderedCollection(id, 0, "", "", []any{})
}

func renderOrderedCollection(id string, totalItems int, first string, last string, orderedItems []any) map[string]any {
	body := map[string]any{
		"id":         id,
		"type":       "OrderedCollection",
		"totalItems": totalItems,
	}
	if first != "" {
		body["first"] = first
	}
	if last != "" {
		body["last"] = last
	}
	if orderedItems != nil {
		body["orderedItems"] = orderedItems
	}
	return withActivityContext(body)
}

func renderOrderedCollectionPage(id string, totalItems int, orderedItems []any, partOf string, prev string, next string) map[string]any {
	body := map[string]any{
		"id":           id,
		"partOf":       partOf,
		"type":         "OrderedCollectionPage",
		"totalItems":   totalItems,
		"orderedItems": orderedItems,
	}
	if prev != "" {
		body["prev"] = prev
	}
	if next != "" {
		body["next"] = next
	}
	return withActivityContext(body)
}

func actorCollectionCount(ctx context.Context, actor *actors.Actor, name string, followLookup FollowLookup) (int, error) {
	if followLookup == nil {
		return 0, nil
	}
	switch name {
	case "followers":
		return followLookup.CountFollowers(ctx, actor.ID)
	case "following":
		return followLookup.CountFollowing(ctx, actor.ID)
	default:
		return 0, nil
	}
}

func actorCollectionItems(ctx context.Context, actor *actors.Actor, name string, followLookup FollowLookup, limit int) ([]any, error) {
	if followLookup == nil {
		return []any{}, nil
	}
	var rows []follows.Follow
	var err error
	switch name {
	case "followers":
		rows, err = followLookup.ListFollowers(ctx, actor.ID, limit)
	case "following":
		rows, err = followLookup.ListFollowing(ctx, actor.ID, limit)
	default:
		return []any{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, len(rows))
	for _, follow := range rows {
		if name == "followers" {
			items = append(items, follow.FollowerURI)
		} else {
			items = append(items, follow.FolloweeURI)
		}
	}
	return items, nil
}

func publicRequestURL(cfg config.Config, r *http.Request) string {
	return strings.TrimRight(cfg.PublicURL, "/") + r.URL.RequestURI()
}

func withActivityContext(body map[string]any) map[string]any {
	body["@context"] = []any{
		"https://www.w3.org/ns/activitystreams",
		"https://w3id.org/security/v1",
		map[string]any{
			"manuallyApprovesFollowers": "as:manuallyApprovesFollowers",
			"sensitive":                 "as:sensitive",
			"Hashtag":                   "as:Hashtag",
			"quoteUrl":                  "as:quoteUrl",
			"toot":                      "http://joinmastodon.org/ns#",
			"Emoji":                     "toot:Emoji",
			"featured":                  "toot:featured",
			"discoverable":              "toot:discoverable",
			"schema":                    "http://schema.org#",
			"PropertyValue":             "schema:PropertyValue",
			"value":                     "schema:value",
			"misskey":                   "https://misskey-hub.net/ns#",
			"_misskey_content":          "misskey:_misskey_content",
			"_misskey_quote":            "misskey:_misskey_quote",
			"isCat":                     "misskey:isCat",
		},
	}
	return body
}

func lookupWebFingerActor(ctx context.Context, cfg config.Config, lookup ActorLookup, resource string) (*actors.Actor, int, error) {
	base := strings.ToLower(strings.TrimRight(cfg.PublicURL, "/"))
	lowerResource := strings.ToLower(resource)
	if strings.HasPrefix(lowerResource, base+"/users/") {
		id := resource[strings.LastIndex(resource, "/")+1:]
		actor, err := lookup.FindLocalByID(ctx, id)
		if actor == nil && err == nil {
			return nil, http.StatusNotFound, nil
		}
		return actor, http.StatusOK, err
	}
	if strings.HasPrefix(lowerResource, base+"/@") {
		username := resource[strings.LastIndex(resource, "/@")+2:]
		actor, err := lookup.FindLocalByUsername(ctx, username)
		if actor == nil && err == nil {
			return nil, http.StatusNotFound, nil
		}
		return actor, http.StatusOK, err
	}
	acct := resource
	if strings.HasPrefix(strings.ToLower(acct), "acct:") {
		acct = acct[len("acct:"):]
	}
	username, host, ok := strings.Cut(acct, "@")
	if !ok {
		return nil, http.StatusBadRequest, nil
	}
	if host != "" && !strings.EqualFold(host, cfg.Host) {
		return nil, http.StatusUnprocessableEntity, nil
	}
	actor, err := lookup.FindLocalByUsername(ctx, username)
	if actor == nil && err == nil {
		return nil, http.StatusNotFound, nil
	}
	return actor, http.StatusOK, err
}

func webFingerJRD(w http.ResponseWriter, cfg config.Config, actor *actors.Actor) {
	w.Header().Set("Content-Type", "application/jrd+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(webFingerBody(cfg, actor))
}

func webFingerXRD(w http.ResponseWriter, cfg config.Config, actor *actors.Actor) {
	body := webFingerBody(cfg, actor)
	links := body["links"].([]map[string]string)
	w.Header().Set("Content-Type", "application/xrd+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0"><Subject>%s</Subject>`, html.EscapeString(body["subject"].(string)))
	for _, link := range links {
		_, _ = fmt.Fprintf(w, `<Link rel="%s"`, html.EscapeString(link["rel"]))
		for _, attr := range []string{"type", "href", "template"} {
			if link[attr] != "" {
				_, _ = fmt.Fprintf(w, ` %s="%s"`, attr, html.EscapeString(link[attr]))
			}
		}
		_, _ = w.Write([]byte("/>"))
	}
	_, _ = w.Write([]byte("</XRD>"))
}

func webFingerBody(cfg config.Config, actor *actors.Actor) map[string]any {
	base := strings.TrimRight(cfg.PublicURL, "/")
	actorURI := actor.URI
	if actorURI == "" {
		actorURI = base + "/users/" + url.PathEscape(actor.ID)
	}
	subject := "acct:" + actor.Username + "@" + cfg.Host
	return map[string]any{
		"subject": subject,
		"links": []map[string]string{
			{
				"rel":  "self",
				"type": "application/activity+json",
				"href": actorURI,
			},
			{
				"rel":  "http://webfinger.net/rel/profile-page",
				"type": "text/html",
				"href": base + "/@" + url.PathEscape(actor.Username),
			},
			{
				"rel":      "http://ostatus.org/schema/1.0/subscribe",
				"template": base + "/authorize-follow?acct={uri}",
			},
		},
	}
}

func accepts(r *http.Request, contentType string) bool {
	accept := r.Header.Get("Accept")
	return accept != "" && strings.Contains(strings.ToLower(accept), strings.ToLower(contentType))
}

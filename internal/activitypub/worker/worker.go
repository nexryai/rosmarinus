package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	apresolver "github.com/nexryai/rosmarinus/internal/activitypub/resolver"
	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type APClient interface {
	FetchObject(context.Context, string, *actors.Actor) (map[string]any, error)
	Deliver(context.Context, string, actors.Actor, map[string]any) error
}

type QueueClient interface {
	Enqueue(context.Context, queue.Task) error
}

type Handler struct {
	cfg        config.Config
	logger     *log.Logger
	repo       actors.Repository
	notes      domainnotes.Repository
	follows    follows.Repository
	queue      QueueClient
	client     APClient
	resolver   *apresolver.Resolver
	localActor *actors.Actor
}

func New(cfg config.Config, logger *log.Logger, repo actors.Repository, noteRepo domainnotes.Repository, followRepo follows.Repository, queueClient QueueClient, apClient APClient, localActor *actors.Actor) *Handler {
	return &Handler{
		cfg:        cfg,
		logger:     logger,
		repo:       repo,
		notes:      noteRepo,
		follows:    followRepo,
		queue:      queueClient,
		client:     apClient,
		resolver:   apresolver.New(repo, apClient, localActor),
		localActor: localActor,
	}
}

func (h *Handler) Register(server *queue.AsynqServer) {
	server.HandleFunc(queue.TaskInbox, h.HandleInboxTask)
	server.HandleFunc(queue.TaskDeliver, h.HandleDeliverTask)
}

func (h *Handler) HandleInboxTask(ctx context.Context, task *asynq.Task) error {
	var payload queue.InboxPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode inbox task: %w", err)
	}
	result, err := h.ProcessInbox(ctx, payload)
	if h.logger != nil {
		if err != nil {
			h.logger.Printf("inbox: failed result=%s err=%v", result, err)
		} else {
			h.logger.Printf("inbox: %s", result)
		}
	}
	return err
}

func (h *Handler) HandleDeliverTask(ctx context.Context, task *asynq.Task) error {
	var payload queue.DeliverPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode deliver task: %w", err)
	}
	actor, err := h.repo.FindLocalByID(ctx, payload.ActorID)
	if err != nil {
		return err
	}
	if actor == nil {
		return fmt.Errorf("deliver actor not found: %s", payload.ActorID)
	}
	if err := h.client.Deliver(ctx, payload.To, *actor, payload.Object); err != nil {
		return err
	}
	if h.logger != nil {
		h.logger.Printf("deliver: sent actor=%s to=%s type=%v", actor.ID, payload.To, payload.Object["type"])
	}
	return nil
}

func (h *Handler) ProcessInbox(ctx context.Context, payload queue.InboxPayload) (string, error) {
	if payload.Version != 1 {
		return "skip: unsupported inbox payload version", nil
	}
	sig, err := signatureFromPayload(payload.Signature)
	if err != nil {
		return "skip: invalid signature payload", nil
	}
	if strings.HasPrefix(strings.ToLower(sig.KeyID), "acct:") {
		return "skip: old acct keyId is not supported", nil
	}
	if _, err := url.ParseRequestURI(sig.KeyID); err != nil {
		return "skip: keyId is not a URL", nil
	}
	actorID, err := aptypes.GetAPID(payload.Activity["actor"])
	if err != nil {
		return "skip: activity actor is invalid", nil
	}
	authActor, err := h.repo.FindByPublicKeyID(ctx, sig.KeyID)
	if err != nil {
		return "", err
	}
	if authActor == nil {
		authActor, err = h.resolver.ResolveActor(ctx, actorID)
		if err != nil {
			return "", fmt.Errorf("resolve actor: %w", err)
		}
	}
	if authActor == nil {
		return "skip: failed to resolve user", nil
	}
	if authActor.PublicKeyPEM == "" {
		return "skip: failed to resolve user publicKey", nil
	}
	if err := apsig.VerifyRSA(sig, authActor.PublicKeyPEM); err != nil {
		return fmt.Sprintf("skip: http-signature verification failed. keyId=%s", sig.KeyID), nil
	}
	if authActor.URI != actorID {
		return fmt.Sprintf("skip: signer actor mismatch signer=%s actor=%s", authActor.URI, actorID), nil
	}
	activityID, ok := payload.Activity["id"].(string)
	if !ok || activityID == "" {
		return "skip: activity.id is not a string", nil
	}
	signerHost, err := hostOf(authActor.URI)
	if err != nil {
		return "skip: signer uri host is invalid", nil
	}
	activityHost, err := hostOf(activityID)
	if err != nil || signerHost != activityHost {
		return fmt.Sprintf("skip: signerHost(%s) != activity.id host(%s)", signerHost, activityHost), nil
	}
	return h.performActivity(ctx, authActor, payload.Activity)
}

func (h *Handler) performActivity(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if actor.IsSuspended {
		return "skip: suspended actor", nil
	}
	if aptypes.IsCollectionOrOrderedCollection(activity) {
		return "skip: refusing to ingest collection as activity", nil
	}
	switch {
	case aptypes.IsCreate(activity):
		return h.performCreate(ctx, actor, activity)
	case aptypes.IsFollow(activity):
		return h.performFollow(ctx, actor, activity)
	case aptypes.IsUndo(activity):
		return h.performUndo(ctx, actor, activity)
	case aptypes.IsDelete(activity):
		return h.performDelete(ctx, actor, activity)
	case aptypes.IsAccept(activity), aptypes.IsReject(activity), aptypes.IsAnnounce(activity), aptypes.IsLike(activity), aptypes.IsUpdate(activity), aptypes.IsBlock(activity), aptypes.IsFlag(activity):
		return fmt.Sprintf("skip: activity type %v is not implemented yet", activity["type"]), nil
	default:
		return fmt.Sprintf("skip: unrecognized activity type %v", activity["type"]), nil
	}
}

func (h *Handler) performCreate(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	object, err := h.createObject(ctx, activity)
	if err != nil {
		return "", err
	}
	if !aptypes.IsPost(object) {
		return fmt.Sprintf("skip: unknown create object type %v", object["type"]), nil
	}
	attributedTo, err := aptypes.GetOneAPID(object["attributedTo"])
	if err != nil {
		return "skip: note attributedTo is invalid", nil
	}
	if actor.URI != attributedTo {
		return "skip: actor.uri !== note.attributedTo", nil
	}
	uri, err := aptypes.GetAPID(object)
	if err != nil {
		return "skip: note.id is not a string", nil
	}
	actorHost, err := hostOf(actor.URI)
	if err != nil {
		return "skip: actor uri host is invalid", nil
	}
	noteHost, err := hostOf(uri)
	if err != nil || actorHost != noteHost {
		return "skip: host in actor.uri !== note.id", nil
	}
	existing, err := h.notes.FindByURI(ctx, uri)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "skip: note exists", nil
	}
	parsed, err := apnotes.ParseRemoteNote(object, uri)
	if err != nil {
		return fmt.Sprintf("skip: invalid note: %v", err), nil
	}
	note := domainnotes.Note{
		URI:            parsed.URI,
		AttributedTo:   parsed.AttributedTo,
		AuthorID:       actor.ID,
		Text:           parsed.Text,
		ContentWarning: parsed.ContentWarning,
		Sensitive:      parsed.Sensitive,
		InReplyToURI:   parsed.InReplyToURI,
		QuoteURI:       parsed.QuoteURI,
		Visibility:     domainnotes.Visibility(parsed.Visibility),
		MentionURIs:    parsed.MentionURIs,
		Hashtags:       parsed.Hashtags,
		Emojis:         parsed.Emojis,
		Attachments:    parsed.Attachments,
		Raw:            object,
		CreatedAt:      time.Now().UTC(),
		PublishedAt:    publishedAt(object),
	}
	if _, err := h.notes.UpsertRemoteNote(ctx, note); err != nil {
		return "", err
	}
	return "ok: note created", nil
}

func (h *Handler) createObject(ctx context.Context, activity map[string]any) (map[string]any, error) {
	object := activity["object"]
	if obj, ok := object.(map[string]any); ok {
		copyCreateAudience(activity, obj)
		if obj["attributedTo"] == nil {
			obj["attributedTo"] = activity["actor"]
		}
		return obj, nil
	}
	uri, err := aptypes.GetAPID(object)
	if err != nil {
		return nil, fmt.Errorf("create object is invalid: %w", err)
	}
	obj, err := h.client.FetchObject(ctx, uri, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve create object: %w", err)
	}
	return obj, nil
}

func (h *Handler) performFollow(ctx context.Context, follower *actors.Actor, activity map[string]any) (string, error) {
	followeeID, err := aptypes.GetAPID(activity["object"])
	if err != nil {
		return "skip: followee not found", nil
	}
	followee, err := h.repo.FindByURI(ctx, followeeID)
	if err != nil {
		return "", err
	}
	if followee == nil || followee.Host != nil {
		return "skip: followee is not a local user", nil
	}
	if h.queue == nil {
		return "skip: queue is not configured", nil
	}
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	activityID, _ := activity["id"].(string)
	if _, err := h.follows.Upsert(ctx, follows.Follow{
		FollowerID:          follower.ID,
		FolloweeID:          followee.ID,
		FollowerURI:         follower.URI,
		FolloweeURI:         followee.URI,
		FollowerHost:        follower.Host,
		FolloweeHost:        followee.Host,
		FollowerInbox:       follower.Inbox,
		FollowerSharedInbox: follower.SharedInbox,
		FolloweeInbox:       followee.Inbox,
		FolloweeSharedInbox: followee.SharedInbox,
		CreatedAt:           time.Now().UTC(),
		RemoteActivityID:    activityID,
	}); err != nil {
		return "", err
	}
	inbox := follower.SharedInbox
	if inbox == "" {
		inbox = follower.Inbox
	}
	if inbox == "" {
		return "skip: follower inbox is empty", nil
	}
	accept := renderAccept(followee, activity)
	task := queue.NewDeliverTask(followee.ID, inbox, accept, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return "", err
	}
	return "ok: follow accepted delivery enqueued", nil
}

func (h *Handler) performUndo(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	object, ok := activity["object"].(map[string]any)
	if !ok || !aptypes.IsFollow(object) {
		return fmt.Sprintf("skip: unsupported undo object type %v", activity["object"]), nil
	}
	followerID, err := aptypes.GetAPID(object["actor"])
	if err != nil {
		return "skip: undo follow actor is invalid", nil
	}
	if followerID != actor.URI {
		return "skip: undo follow actor mismatch", nil
	}
	followeeID, err := aptypes.GetAPID(object["object"])
	if err != nil {
		return "skip: undo followee not found", nil
	}
	followee, err := h.repo.FindByURI(ctx, followeeID)
	if err != nil {
		return "", err
	}
	if followee == nil || followee.Host != nil {
		return "skip: undo followee is not a local user", nil
	}
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	undoID, _ := activity["id"].(string)
	if err := h.follows.Delete(ctx, actor.ID, followee.ID, undoID); err != nil {
		return "", err
	}
	return "ok: unfollowed", nil
}

func (h *Handler) performDelete(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	if activityActor, err := aptypes.GetAPID(activity["actor"]); err != nil || activityActor != actor.URI {
		return "skip: delete actor mismatch", nil
	}
	object := activity["object"]
	uri, err := aptypes.GetAPID(object)
	if err != nil {
		return "skip: delete object id is invalid", nil
	}
	formerType := deleteObjectFormerType(object)
	if formerType == "" {
		if uri == actor.URI {
			formerType = "Person"
		} else {
			formerType = "Note"
		}
	}
	if !isDeletePostType(formerType) {
		if _, ok := aptypes.ValidActorTypes[formerType]; ok {
			return fmt.Sprintf("skip: delete actor is not implemented yet: %s", uri), nil
		}
		return fmt.Sprintf("skip: unknown delete object type %s", formerType), nil
	}
	note, err := h.notes.FindByURI(ctx, uri)
	if err != nil {
		return "", err
	}
	if note == nil {
		return "skip: note not found", nil
	}
	if note.AuthorID != actor.ID {
		return "skip: delete actor is not note author", nil
	}
	if err := h.notes.DeleteRemoteNote(ctx, uri, actor.ID); err != nil {
		return "", err
	}
	return "ok: note deleted", nil
}

func deleteObjectFormerType(object any) string {
	obj, ok := object.(map[string]any)
	if !ok {
		return ""
	}
	if aptypes.IsType(obj, "Tombstone") {
		return firstString(obj["formerType"])
	}
	return firstString(obj["type"])
}

func firstString(value any) string {
	items := aptypes.ToArray(value)
	if len(items) == 0 {
		return ""
	}
	if s, ok := items[0].(string); ok {
		return s
	}
	return ""
}

func isDeletePostType(typ string) bool {
	_, ok := aptypes.ValidPostTypes[typ]
	return ok
}

func copyCreateAudience(activity map[string]any, object map[string]any) {
	to := uniqueValues(append(aptypes.ToArray(activity["to"]), aptypes.ToArray(object["to"])...))
	cc := uniqueValues(append(aptypes.ToArray(activity["cc"]), aptypes.ToArray(object["cc"])...))
	if len(to) > 0 {
		activity["to"] = to
		object["to"] = to
	}
	if len(cc) > 0 {
		activity["cc"] = cc
		object["cc"] = cc
	}
}

func uniqueValues(items []any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0, len(items))
	for _, item := range items {
		key := fmt.Sprintf("%v", item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func publishedAt(object map[string]any) *time.Time {
	raw, ok := object["published"].(string)
	if !ok || raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &t
}

func renderAccept(localActor *actors.Actor, follow map[string]any) map[string]any {
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":     strings.TrimRight(localActor.URI, "/") + "#accepts/" + shortID(follow["id"]),
		"type":   "Accept",
		"actor":  localActor.URI,
		"object": follow,
	}
}

func signatureFromPayload(payload map[string]any) (apsig.HTTPSignature, error) {
	keyID, _ := payload["keyId"].(string)
	algorithm, _ := payload["algorithm"].(string)
	signingString, _ := payload["signingString"].(string)
	rawSignature, _ := payload["signature"].(string)
	if keyID == "" || algorithm == "" || signingString == "" || rawSignature == "" {
		return apsig.HTTPSignature{}, fmt.Errorf("missing signature fields")
	}
	if !apsig.IsSupportedAlgorithm(algorithm) {
		return apsig.HTTPSignature{}, fmt.Errorf("unsupported signature algorithm: %s", algorithm)
	}
	decoded, err := base64.StdEncoding.DecodeString(rawSignature)
	if err != nil {
		return apsig.HTTPSignature{}, err
	}
	headers := stringsFromAny(payload["headers"])
	return apsig.HTTPSignature{
		KeyID:         keyID,
		Algorithm:     algorithm,
		Headers:       headers,
		Signature:     decoded,
		SigningString: signingString,
	}, nil
}

func stringsFromAny(value any) []string {
	if items, ok := value.([]string); ok {
		return items
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid url: %s", raw)
	}
	return strings.ToLower(u.Hostname()), nil
}

func shortID(value any) string {
	if s, ok := value.(string); ok && s != "" {
		parts := strings.Split(strings.TrimRight(s, "/"), "/")
		return parts[len(parts)-1]
	}
	return "activity"
}

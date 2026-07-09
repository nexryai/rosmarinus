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
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
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
	blocks     blocks.Repository
	reactions  reactions.Repository
	queue      QueueClient
	client     APClient
	resolver   *apresolver.Resolver
	localActor *actors.Actor
}

func New(cfg config.Config, logger *log.Logger, repo actors.Repository, noteRepo domainnotes.Repository, followRepo follows.Repository, blockRepo blocks.Repository, reactionRepo reactions.Repository, queueClient QueueClient, apClient APClient, localActor *actors.Actor) *Handler {
	return &Handler{
		cfg:        cfg,
		logger:     logger,
		repo:       repo,
		notes:      noteRepo,
		follows:    followRepo,
		blocks:     blockRepo,
		reactions:  reactionRepo,
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
	case aptypes.IsLike(activity):
		return h.performLike(ctx, actor, activity)
	case aptypes.IsAnnounce(activity):
		return h.performAnnounce(ctx, actor, activity)
	case aptypes.IsBlock(activity):
		return h.performBlock(ctx, actor, activity)
	case aptypes.IsAccept(activity), aptypes.IsReject(activity), aptypes.IsUpdate(activity), aptypes.IsFlag(activity):
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

func (h *Handler) performBlock(ctx context.Context, blocker *actors.Actor, activity map[string]any) (string, error) {
	blockeeURI, err := aptypes.GetAPID(activity["object"])
	if err != nil {
		return "skip: blockee not found", nil
	}
	blockee, err := h.repo.FindByURI(ctx, blockeeURI)
	if err != nil {
		return "", err
	}
	if blockee == nil {
		return "skip: blockee not found", nil
	}
	if blockee.Host != nil {
		return "skip: blockee is not a local user", nil
	}
	if h.blocks == nil {
		return "skip: block repository is not configured", nil
	}
	activityID, _ := activity["id"].(string)
	if _, err := h.blocks.Upsert(ctx, blocks.Block{
		BlockerID:        blocker.ID,
		BlockeeID:        blockee.ID,
		BlockerURI:       blocker.URI,
		BlockeeURI:       blockee.URI,
		BlockerHost:      blocker.Host,
		BlockeeHost:      blockee.Host,
		CreatedAt:        time.Now().UTC(),
		RemoteActivityID: activityID,
	}); err != nil {
		return "", err
	}
	return "ok", nil
}

func (h *Handler) performLike(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	if h.reactions == nil {
		return "skip: reaction repository is not configured", nil
	}
	targetURI, err := aptypes.GetAPID(activity["object"])
	if err != nil {
		return "skip: target note is invalid", nil
	}
	note, err := h.notes.FindByURI(ctx, targetURI)
	if err != nil {
		return "", err
	}
	if note == nil {
		return fmt.Sprintf("skip: target note not found %s", targetURI), nil
	}
	reaction := reactionFromActivity(activity)
	existing, err := h.reactions.Find(ctx, note.ID, actor.ID)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Reaction == reaction {
		return "skip: already reacted", nil
	}
	activityID, _ := activity["id"].(string)
	if _, err := h.reactions.Upsert(ctx, reactions.Reaction{
		NoteID:           note.ID,
		NoteURI:          note.URI,
		ActorID:          actor.ID,
		ActorURI:         actor.URI,
		ActorHost:        actor.Host,
		Reaction:         reaction,
		RemoteActivityID: activityID,
		CreatedAt:        time.Now().UTC(),
	}); err != nil {
		return "", err
	}
	return "ok: reaction created", nil
}

func (h *Handler) performAnnounce(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	activityID, err := aptypes.GetAPID(activity)
	if err != nil {
		return "skip: announce id is invalid", nil
	}
	existing, err := h.notes.FindByURI(ctx, activityID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "skip: announce exists", nil
	}
	targetURI, err := aptypes.GetAPID(activity["object"])
	if err != nil {
		return "skip: announce target is invalid", nil
	}
	target, err := h.resolveAnnounceTarget(ctx, targetURI)
	if err != nil {
		return "", err
	}
	if target == nil {
		return fmt.Sprintf("skip: announce target not found %s", targetURI), nil
	}
	note := domainnotes.Note{
		URI:          activityID,
		AttributedTo: actor.URI,
		AuthorID:     actor.ID,
		Visibility:   domainnotes.Visibility(apnotes.ParseVisibility(actor.URI, activity["to"], activity["cc"])),
		RenoteID:     target.ID,
		RenoteURI:    target.URI,
		Raw:          activity,
		CreatedAt:    time.Now().UTC(),
		PublishedAt:  publishedAt(activity),
	}
	if _, err := h.notes.UpsertRemoteNote(ctx, note); err != nil {
		return "", err
	}
	return "ok: announce created", nil
}

func (h *Handler) resolveAnnounceTarget(ctx context.Context, targetURI string) (*domainnotes.Note, error) {
	target, err := h.notes.FindByURI(ctx, targetURI)
	if err != nil || target != nil {
		return target, err
	}
	if h.client == nil {
		return nil, fmt.Errorf("announce target resolver is not configured")
	}
	object, err := h.client.FetchObject(ctx, targetURI, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve announce target: %w", err)
	}
	if !aptypes.IsPost(object) {
		return nil, fmt.Errorf("announce target is not a post: %v", object["type"])
	}
	attributedTo, err := aptypes.GetOneAPID(object["attributedTo"])
	if err != nil {
		return nil, fmt.Errorf("announce target attributedTo is invalid: %w", err)
	}
	if h.repo == nil {
		return nil, fmt.Errorf("actor repository is not configured")
	}
	targetActor, err := h.repo.FindByURI(ctx, attributedTo)
	if err != nil {
		return nil, err
	}
	if targetActor == nil {
		targetActor, err = h.resolver.ResolveActor(ctx, attributedTo)
		if err != nil {
			return nil, fmt.Errorf("resolve announce target actor: %w", err)
		}
	}
	if targetActor == nil {
		return nil, nil
	}
	parsed, err := apnotes.ParseRemoteNote(object, targetURI)
	if err != nil {
		return nil, fmt.Errorf("invalid announce target note: %w", err)
	}
	return h.notes.UpsertRemoteNote(ctx, domainnotes.Note{
		URI:            parsed.URI,
		AttributedTo:   parsed.AttributedTo,
		AuthorID:       targetActor.ID,
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
	})
}

func (h *Handler) performUndo(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	object, err := h.undoObject(ctx, activity["object"])
	if err != nil {
		return "", err
	}
	if object == nil {
		return fmt.Sprintf("skip: unsupported undo object type %v", activity["object"]), nil
	}
	if aptypes.IsLike(object) {
		return h.performUndoLike(ctx, actor, activity, object)
	}
	if aptypes.IsAnnounce(object) {
		return h.performUndoAnnounce(ctx, actor, object)
	}
	if aptypes.IsBlock(object) {
		return h.performUndoBlock(ctx, actor, activity, object)
	}
	if !aptypes.IsFollow(object) {
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

func (h *Handler) undoObject(ctx context.Context, value any) (map[string]any, error) {
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	uri, err := aptypes.GetAPID(value)
	if err != nil {
		return nil, nil
	}
	if h.client == nil {
		return nil, fmt.Errorf("undo object resolver is not configured")
	}
	object, err := h.client.FetchObject(ctx, uri, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve undo object: %w", err)
	}
	return object, nil
}

func (h *Handler) performUndoLike(ctx context.Context, actor *actors.Actor, activity, object map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	if h.reactions == nil {
		return "skip: reaction repository is not configured", nil
	}
	reacterID, err := aptypes.GetAPID(object["actor"])
	if err != nil {
		return "skip: undo like actor is invalid", nil
	}
	if reacterID != actor.URI {
		return "skip: undo like actor mismatch", nil
	}
	targetURI, err := aptypes.GetAPID(object["object"])
	if err != nil {
		return "skip: target note is invalid", nil
	}
	note, err := h.notes.FindByURI(ctx, targetURI)
	if err != nil {
		return "", err
	}
	if note == nil {
		return fmt.Sprintf("skip: target note not found %s", targetURI), nil
	}
	undoID, _ := activity["id"].(string)
	if err := h.reactions.Delete(ctx, note.ID, actor.ID, undoID); err != nil {
		return "", err
	}
	return "ok: reaction deleted", nil
}

func (h *Handler) performUndoAnnounce(ctx context.Context, actor *actors.Actor, object map[string]any) (string, error) {
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	announcerID, err := aptypes.GetAPID(object["actor"])
	if err != nil {
		return "skip: undo announce actor is invalid", nil
	}
	if announcerID != actor.URI {
		return "skip: undo announce actor mismatch", nil
	}
	announceURI, err := aptypes.GetAPID(object)
	if err != nil {
		return "skip: undo announce id is invalid", nil
	}
	note, err := h.notes.FindByURI(ctx, announceURI)
	if err != nil {
		return "", err
	}
	if note == nil || note.AuthorID != actor.ID {
		return "skip: no such Announce", nil
	}
	if err := h.notes.DeleteRemoteNote(ctx, announceURI, actor.ID); err != nil {
		return "", err
	}
	return "ok: deleted", nil
}

func (h *Handler) performUndoBlock(ctx context.Context, blocker *actors.Actor, activity, object map[string]any) (string, error) {
	blockerID, err := aptypes.GetAPID(object["actor"])
	if err != nil {
		return "skip: undo block actor is invalid", nil
	}
	if blockerID != blocker.URI {
		return "skip: undo block actor mismatch", nil
	}
	blockeeURI, err := aptypes.GetAPID(object["object"])
	if err != nil {
		return "skip: blockee not found", nil
	}
	blockee, err := h.repo.FindByURI(ctx, blockeeURI)
	if err != nil {
		return "", err
	}
	if blockee == nil {
		return "skip: blockee not found", nil
	}
	if blockee.Host != nil {
		return "skip: blockee is not a local user", nil
	}
	if h.blocks == nil {
		return "skip: block repository is not configured", nil
	}
	undoID, _ := activity["id"].(string)
	if err := h.blocks.Delete(ctx, blocker.ID, blockee.ID, undoID); err != nil {
		return "", err
	}
	return "ok", nil
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

func reactionFromActivity(activity map[string]any) string {
	for _, field := range []string{"_misskey_reaction", "content", "name"} {
		if reaction := firstString(activity[field]); reaction != "" {
			return reaction
		}
	}
	return "like"
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

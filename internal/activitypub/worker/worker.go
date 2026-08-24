package worker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	appolls "github.com/nexryai/rosmarinus/internal/activitypub/polls"
	apreactions "github.com/nexryai/rosmarinus/internal/activitypub/reactions"
	apresolver "github.com/nexryai/rosmarinus/internal/activitypub/resolver"
	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	apwebfinger "github.com/nexryai/rosmarinus/internal/activitypub/webfinger"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/cleanup"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/instances"
	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/domain/reports"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type APClient interface {
	FetchObject(context.Context, string, *actors.Actor) (map[string]any, error)
	Deliver(context.Context, string, actors.Actor, map[string]any) (int, error)
}

const (
	postDeliveryFollowerLimit = 100
	collectionActivityLimit   = 256
)

type QueueClient interface {
	Enqueue(context.Context, queue.Task) error
}

type ActivityLocker interface {
	Acquire(context.Context, string) (func(context.Context) error, bool, error)
}

type ConnectorPublisher interface {
	PublishPostCreated(context.Context, connector.PostCreated) error
	PublishNotificationCreated(context.Context, connector.NotificationCreated) error
	PublishFollowApprovalRequested(context.Context, connector.FollowApproval) error
	PublishFollowApprovalCompleted(context.Context, connector.FollowApproval) error
	PublishFollowApprovalRejected(context.Context, connector.FollowApproval) error
}

type MediaFetcher interface {
	Fetch(context.Context, string) (mediafetch.Result, error)
	ValidateURL(*url.URL) error
}

type InstanceMetadataFetcher interface {
	Fetch(context.Context, string) (instances.Metadata, error)
}

type Handler struct {
	cfg             config.Config
	logger          *log.Logger
	repo            actors.Repository
	notes           domainnotes.Repository
	follows         follows.Repository
	blocks          blocks.Repository
	emojis          emojis.Repository
	reactions       reactions.Repository
	reports         reports.Repository
	notifications   notifications.Repository
	polls           polls.Repository
	cleanup         cleanup.Repository
	media           domainmedia.Repository
	mediaFetcher    MediaFetcher
	instances       instances.Repository
	metadataFetcher InstanceMetadataFetcher
	queue           QueueClient
	client          APClient
	connector       ConnectorPublisher
	locker          ActivityLocker
	resolver        *apresolver.Resolver
	localActor      *actors.Actor
}

func New(cfg config.Config, logger *log.Logger, repo actors.Repository, noteRepo domainnotes.Repository, followRepo follows.Repository, blockRepo blocks.Repository, reactionRepo reactions.Repository, reportRepo reports.Repository, queueClient QueueClient, apClient APClient, localActor *actors.Actor) *Handler {
	actorResolver := apresolver.NewWithWebFinger(repo, apClient, localActor, apwebfinger.New(nil, cfg.UserAgent))
	actorResolver.SetFederationPolicy(cfg)
	actorResolver.SetNoteRepository(noteRepo)
	return &Handler{
		cfg:        cfg,
		logger:     logger,
		repo:       repo,
		notes:      noteRepo,
		follows:    followRepo,
		blocks:     blockRepo,
		reactions:  reactionRepo,
		reports:    reportRepo,
		queue:      queueClient,
		client:     apClient,
		resolver:   actorResolver,
		localActor: localActor,
	}
}

func (h *Handler) SetConnectorPublisher(publisher ConnectorPublisher) {
	h.connector = publisher
}

func (h *Handler) SetNotificationRepository(repository notifications.Repository) {
	h.notifications = repository
}

func (h *Handler) SetEmojiRepository(repository emojis.Repository) {
	h.emojis = repository
	h.resolver.SetEmojiRepository(repository)
}

func (h *Handler) SetPollRepository(repository polls.Repository) {
	h.polls = repository
	h.resolver.SetPollRepository(repository)
}

func (h *Handler) SetAccountCleanupRepository(repository cleanup.Repository) {
	h.cleanup = repository
}

func (h *Handler) SetMediaRepository(repository domainmedia.Repository, fetcher MediaFetcher) {
	h.media = repository
	h.mediaFetcher = fetcher
	h.resolver.SetMediaScheduler(h)
}

func (h *Handler) SetInstanceRepository(repository instances.Repository, fetcher InstanceMetadataFetcher) {
	h.instances = repository
	h.metadataFetcher = fetcher
}

func (h *Handler) MarkNotificationRead(ctx context.Context, accountID, actorID, notificationID string) (connector.NotificationRead, error) {
	if h.notifications == nil {
		return connector.NotificationRead{}, fmt.Errorf("notification repository is not configured")
	}
	notification, err := h.notifications.MarkRead(ctx, strings.TrimSpace(accountID), strings.TrimSpace(actorID), strings.TrimSpace(notificationID))
	if err != nil {
		return connector.NotificationRead{}, err
	}
	if notification == nil {
		return connector.NotificationRead{}, fmt.Errorf("notification not found")
	}
	return connector.NotificationRead{NotificationID: notification.ID, IsRead: notification.IsRead}, nil
}

func (h *Handler) SetActivityLocker(locker ActivityLocker) {
	h.locker = locker
	h.resolver.SetObjectLocker(locker)
}

func (h *Handler) Register(server *queue.AsynqServer) {
	server.HandleFunc(queue.TaskInbox, h.HandleInboxTask)
	server.HandleFunc(queue.TaskDeliver, h.HandleDeliverTask)
	server.HandleFunc(queue.TaskAccountDelete, h.HandleAccountDeleteTask)
	server.HandleFunc(queue.TaskPollEnded, h.HandlePollEndedTask)
	server.HandleFunc(queue.TaskMedia, h.HandleMediaFetchTask)
	server.HandleFunc(queue.TaskMetadata, h.HandleMetadataTask)
}

func (h *Handler) HandleMetadataTask(ctx context.Context, task *asynq.Task) error {
	var payload queue.MetadataPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode instance metadata task: %w", err)
	}
	if payload.Version != 1 || strings.TrimSpace(payload.Host) == "" {
		return fmt.Errorf("invalid instance metadata task payload")
	}
	if h.instances == nil || h.metadataFetcher == nil {
		return fmt.Errorf("instance repository and metadata fetcher are required")
	}
	instance, _, err := h.instances.Register(ctx, payload.Host, time.Now().UTC())
	if err != nil {
		return err
	}
	if !payload.Force && instance.InfoUpdatedAt != nil && time.Since(*instance.InfoUpdatedAt) < 24*time.Hour {
		return nil
	}
	var release func(context.Context) error
	if h.locker != nil {
		var acquired bool
		release, acquired, err = h.locker.Acquire(ctx, "metadata:"+instance.Host)
		if err != nil {
			return err
		}
		if !acquired {
			return nil
		}
		defer release(context.Background())
	}
	metadata, err := h.metadataFetcher.Fetch(ctx, instance.Host)
	if err != nil {
		return err
	}
	updated, err := h.instances.UpdateMetadata(ctx, instance.Host, metadata, time.Now().UTC())
	if err != nil {
		return err
	}
	_ = h.ScheduleMedia(ctx, updated.IconURL)
	_ = h.ScheduleMedia(ctx, updated.FaviconURL)
	if h.logger != nil {
		h.logger.Printf("instance: metadata updated host=%s software=%s version=%s", updated.Host, updated.SoftwareName, updated.SoftwareVersion)
	}
	return nil
}

func (h *Handler) ScheduleMedia(ctx context.Context, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	if h.media == nil || h.queue == nil {
		return nil
	}
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	if h.mediaFetcher == nil {
		return fmt.Errorf("media fetcher is not configured")
	}
	if err := h.mediaFetcher.ValidateURL(target); err != nil {
		return err
	}
	publicBase := strings.TrimRight(h.cfg.PublicURL, "/") + "/media/"
	mediaRecord, err := h.media.UpsertPending(ctx, target.String(), publicBase+domainmedia.IDForURL(target.String()))
	if err != nil {
		return err
	}
	if mediaRecord.State == domainmedia.StateReady {
		return nil
	}
	return h.queue.Enqueue(ctx, queue.NewMediaFetchTask(mediaRecord.ID, mediaRecord.OriginalURL))
}

func (h *Handler) HandleMediaFetchTask(ctx context.Context, task *asynq.Task) error {
	var payload queue.MediaFetchPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode media fetch task: %w", err)
	}
	if payload.Version != 1 || payload.MediaID == "" || payload.URL == "" {
		return fmt.Errorf("invalid media fetch task payload")
	}
	if h.media == nil || h.mediaFetcher == nil {
		return fmt.Errorf("media repository and fetcher are required")
	}
	record, err := h.media.FindByID(ctx, payload.MediaID)
	if err != nil {
		return err
	}
	if record == nil || record.OriginalURL != payload.URL {
		return fmt.Errorf("media fetch task does not match stored source")
	}
	if record.State == domainmedia.StateReady {
		return nil
	}
	var release func(context.Context) error
	if h.locker != nil {
		var acquired bool
		release, acquired, err = h.locker.Acquire(ctx, "media:"+record.ID)
		if err != nil {
			return err
		}
		if !acquired {
			return fmt.Errorf("media fetch is already in progress")
		}
		defer release(context.Background())
	}
	result, err := h.mediaFetcher.Fetch(ctx, record.OriginalURL)
	if err != nil {
		_ = h.media.MarkFailed(ctx, record.ID, err.Error())
		return err
	}
	digestBytes := sha256.Sum256(result.Body)
	digest := hex.EncodeToString(digestBytes[:])
	if err := h.media.StoreBlob(ctx, record.ID, bytes.NewReader(result.Body), result.ContentType, int64(len(result.Body)), digest); err != nil {
		_ = h.media.MarkFailed(ctx, record.ID, err.Error())
		return err
	}
	if _, err := h.media.MarkReady(ctx, record.ID, result.ContentType, int64(len(result.Body)), digest); err != nil {
		return err
	}
	if h.logger != nil {
		h.logger.Printf("media: cached id=%s bytes=%d content_type=%s", record.ID, len(result.Body), result.ContentType)
	}
	return nil
}

func (h *Handler) HandleAccountDeleteTask(ctx context.Context, task *asynq.Task) error {
	var payload queue.AccountDeletePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode account delete task: %w", err)
	}
	if payload.Version != 1 || payload.ActorID == "" || payload.ActorURI == "" {
		return fmt.Errorf("invalid account delete task payload")
	}
	if h.cleanup == nil {
		return fmt.Errorf("account cleanup repository is not configured")
	}
	actor, err := h.repo.FindAnyByURI(ctx, payload.ActorURI)
	if err != nil {
		return err
	}
	if actor == nil {
		return nil
	}
	if actor.ID != payload.ActorID || actor.Host == nil || !actor.IsSuspended {
		return fmt.Errorf("account delete task actor does not match a deleted remote actor")
	}
	result, err := h.cleanup.CleanupRemoteActor(ctx, actor.ID)
	if err != nil {
		return err
	}
	if h.logger != nil {
		h.logger.Printf("account-delete: cleaned actor=%s notes=%d reactions=%d follows=%d blocks=%d polls=%d notifications=%d", actor.ID, result.Notes, result.Reactions, result.Follows, result.Blocks, result.Polls, result.Notifications)
	}
	return nil
}

func (h *Handler) HandlePollEndedTask(ctx context.Context, task *asynq.Task) error {
	var payload queue.PollEndedPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode poll ended task: %w", err)
	}
	if payload.Version != 1 || payload.NoteID == "" {
		return fmt.Errorf("invalid poll ended task payload")
	}
	if h.notes == nil || h.polls == nil {
		return fmt.Errorf("note and poll repositories are required")
	}
	note, err := h.notes.FindByID(ctx, payload.NoteID)
	if err != nil || note == nil {
		return err
	}
	poll, err := h.polls.FindByNoteID(ctx, note.ID)
	if err != nil || poll == nil {
		return err
	}
	if poll.AuthorHost != nil || poll.ExpiresAt == nil {
		return nil
	}
	now := time.Now().UTC()
	if now.Before(*poll.ExpiresAt) {
		if h.queue == nil {
			return fmt.Errorf("queue is required to reschedule poll ended task")
		}
		return h.queue.Enqueue(ctx, queue.NewPollEndedTask(note.ID, poll.ExpiresAt.Sub(now)))
	}
	owner, err := h.repo.FindLocalByID(ctx, poll.AuthorID)
	if err != nil || owner == nil {
		return err
	}
	voterIDs, err := h.polls.ListVoterActorIDs(ctx, note.ID)
	if err != nil {
		return err
	}
	recipientIDs := append([]string{owner.ID}, voterIDs...)
	seen := make(map[string]struct{}, len(recipientIDs))
	for _, actorID := range recipientIDs {
		if _, exists := seen[actorID]; exists {
			continue
		}
		seen[actorID] = struct{}{}
		recipient, err := h.repo.FindLocalByID(ctx, actorID)
		if err != nil {
			return err
		}
		if recipient == nil {
			continue
		}
		if err := h.createNotification(ctx, recipient, notifications.KindPollEnded, owner, note.ID, "poll-ended:"+note.ID); err != nil {
			return err
		}
	}
	return nil
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
	target, err := url.Parse(payload.To)
	if err != nil || target.Hostname() == "" {
		return fmt.Errorf("invalid delivery target")
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if h.instances != nil {
		instance, _, registerErr := h.instances.Register(ctx, host, time.Now().UTC())
		if registerErr != nil {
			return registerErr
		}
		if instance.SuspensionState != "" && instance.SuspensionState != instances.SuspensionNone {
			if h.logger != nil {
				h.logger.Printf("deliver: skipped suspended instance host=%s state=%s", host, instance.SuspensionState)
			}
			return nil
		}
	}
	status, err := h.client.Deliver(ctx, payload.To, *actor, payload.Object)
	if err != nil {
		var statusError interface{ HTTPStatusCode() int }
		if status == 0 && errors.As(err, &statusError) {
			status = statusError.HTTPStatusCode()
		}
		if h.instances != nil {
			if _, recordErr := h.instances.RecordDeliveryFailure(ctx, host, time.Now().UTC(), status); recordErr != nil && h.logger != nil {
				h.logger.Printf("instance: record delivery failure host=%s error=%v", host, recordErr)
			}
			if payload.IsSharedInbox && status == 410 {
				if _, suspendErr := h.instances.SuspendGone(ctx, host, time.Now().UTC()); suspendErr != nil {
					return fmt.Errorf("suspend gone instance %s: %w", host, suspendErr)
				}
			}
		}
		if status >= 300 && !retryableDeliveryStatus(status) {
			return fmt.Errorf("%v: %w", err, asynq.SkipRetry)
		}
		return err
	}
	if h.instances != nil {
		if instance, recordErr := h.instances.RecordDeliverySuccess(ctx, host, time.Now().UTC(), status); recordErr != nil {
			if h.logger != nil {
				h.logger.Printf("instance: record delivery success host=%s error=%v", host, recordErr)
			}
		} else {
			h.scheduleInstanceMetadata(ctx, instance)
		}
	}
	if h.logger != nil {
		h.logger.Printf("deliver: sent actor=%s to=%s type=%v", actor.ID, payload.To, payload.Object["type"])
	}
	return nil
}

func retryableDeliveryStatus(status int) bool {
	return status == 408 || status == 429 || status >= 500
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
	keyURL, err := url.ParseRequestURI(sig.KeyID)
	if err != nil || keyURL.Hostname() == "" {
		return "skip: keyId is not a URL", nil
	}
	if h.cfg.IsFederationHostBlocked(keyURL.Hostname()) {
		return fmt.Sprintf("skip: blocked request host=%s", keyURL.Hostname()), nil
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
	} else {
		authActor, err = h.resolver.ResolveActor(ctx, authActor.URI)
		if err != nil {
			return "", fmt.Errorf("refresh actor: %w", err)
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
	if h.instances != nil {
		if instance, recordErr := h.instances.RecordReceived(ctx, signerHost, time.Now().UTC()); recordErr != nil {
			if h.logger != nil {
				h.logger.Printf("instance: record inbox host=%s error=%v", signerHost, recordErr)
			}
		} else {
			h.scheduleInstanceMetadata(ctx, instance)
		}
	}
	if h.locker != nil {
		lockName := fmt.Sprintf("activity:%x", sha256.Sum256([]byte(activityID)))
		unlock, acquired, err := h.locker.Acquire(ctx, lockName)
		if err != nil {
			return "", fmt.Errorf("acquire activity lock: %w", err)
		}
		if !acquired {
			return "skip: activity is already being processed", nil
		}
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := unlock(unlockCtx); err != nil && h.logger != nil {
				h.logger.Printf("inbox: release activity lock id=%s err=%v", activityID, err)
			}
		}()
	}
	return h.performActivity(ctx, authActor, payload.Activity)
}

func (h *Handler) scheduleInstanceMetadata(ctx context.Context, instance *instances.Instance) {
	if h.queue == nil || instance == nil {
		return
	}
	if instance.InfoUpdatedAt != nil && time.Since(*instance.InfoUpdatedAt) < 24*time.Hour {
		return
	}
	if err := h.queue.Enqueue(ctx, queue.NewMetadataTask(instance.Host, false)); err != nil && h.logger != nil {
		h.logger.Printf("instance: enqueue metadata host=%s error=%v", instance.Host, err)
	}
}

func (h *Handler) performActivity(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	return h.performActivityWithResolution(ctx, actor, activity, &activityResolution{fetched: map[string]struct{}{}})
}

type activityResolution struct {
	fetched map[string]struct{}
	depth   int
}

func (h *Handler) performActivityWithResolution(ctx context.Context, actor *actors.Actor, activity map[string]any, resolution *activityResolution) (string, error) {
	if actor.IsSuspended {
		return "skip: suspended actor", nil
	}
	if aptypes.IsCollectionOrOrderedCollection(activity) {
		if resolution.depth >= collectionActivityLimit {
			return "skip: collection would surpass recursion limit", nil
		}
		resolution.depth++
		defer func() { resolution.depth-- }()
		return h.performCollection(ctx, actor, activity, resolution)
	}
	return h.performOneActivity(ctx, actor, activity)
}

func (h *Handler) performCollection(ctx context.Context, actor *actors.Actor, collection map[string]any, resolution *activityResolution) (string, error) {
	items := collection["items"]
	if aptypes.IsOrderedCollection(collection) {
		items = collection["orderedItems"]
	}
	activities := aptypes.ToArray(items)
	if len(activities) >= collectionActivityLimit {
		return "skip: collection would surpass recursion limit", nil
	}
	actorHost, err := hostOf(actor.URI)
	if err != nil {
		return "skip: collection actor uri host is invalid", nil
	}

	reasons := make([]string, 0)
	for _, item := range activities {
		activity, err := h.resolveCollectionActivity(ctx, item, resolution)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("%v: %v", item, err))
			continue
		}
		activityID, err := aptypes.GetAPID(activity)
		if err != nil {
			reasons = append(reasons, "unknown: activity id is missing")
			continue
		}
		activityHost, err := hostOf(activityID)
		if err != nil || activityHost != actorHost {
			reasons = append(reasons, activityID+": activity id host mismatches signer")
			continue
		}
		result, err := h.performActivityWithResolution(ctx, actor, activity, resolution)
		if err != nil {
			if h.logger != nil {
				h.logger.Printf("inbox: collection item failed id=%s err=%v", activityID, err)
			}
			reasons = append(reasons, activityID+": "+err.Error())
			continue
		}
		if result != "" && !strings.HasPrefix(result, "ok") {
			reasons = append(reasons, activityID+": "+result)
		}
	}
	if len(reasons) > 0 {
		return strings.Join(reasons, "\n"), nil
	}
	return "ok: collection processed", nil
}

func (h *Handler) resolveCollectionActivity(ctx context.Context, value any, resolution *activityResolution) (map[string]any, error) {
	if activity, ok := value.(map[string]any); ok {
		return activity, nil
	}
	activityID, err := aptypes.GetAPID(value)
	if err != nil {
		return nil, fmt.Errorf("collection item is invalid: %w", err)
	}
	if _, ok := resolution.fetched[activityID]; ok {
		return nil, fmt.Errorf("cannot resolve already resolved activity: %s", activityID)
	}
	if len(resolution.fetched) >= collectionActivityLimit {
		return nil, fmt.Errorf("collection resolution limit reached")
	}
	resolution.fetched[activityID] = struct{}{}
	if h.client == nil {
		return nil, fmt.Errorf("collection item resolver is not configured")
	}
	activity, err := h.client.FetchObject(ctx, activityID, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve collection item: %w", err)
	}
	if activity == nil {
		return nil, fmt.Errorf("resolved collection item is empty")
	}
	return activity, nil
}

func (h *Handler) performOneActivity(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
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
	case aptypes.IsFlag(activity):
		return h.performFlag(ctx, actor, activity)
	case aptypes.IsAccept(activity):
		return h.performAcceptFollow(ctx, actor, activity)
	case aptypes.IsReject(activity):
		return h.performRejectFollow(ctx, actor, activity)
	case aptypes.IsUpdate(activity):
		return h.performUpdate(ctx, actor, activity)
	case aptypes.IsMove(activity):
		return h.performMove(ctx, actor, activity)
	default:
		return fmt.Sprintf("skip: unrecognized activity type %v", activity["type"]), nil
	}
}

func (h *Handler) performUpdate(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	if actor.Host == nil {
		return "skip: update actor is not remote", nil
	}
	object, err := h.updateObject(ctx, activity["object"])
	if err != nil {
		return "", err
	}
	if object == nil {
		return "skip: update object is empty", nil
	}
	if aptypes.IsType(object, "Question") {
		return h.performUpdateQuestion(ctx, actor, object)
	}
	if !aptypes.IsActor(object) {
		return fmt.Sprintf("skip: update object type %v is not implemented", activity["object"]), nil
	}
	objectID, err := aptypes.GetAPID(object)
	if err != nil || objectID != actor.URI {
		return "skip: actor id mismatch", nil
	}
	updated, err := apresolver.ParseRemoteActor(object, actor.URI)
	if err != nil {
		return "", fmt.Errorf("parse updated actor: %w", err)
	}
	if updated.PublicKeyID == "" {
		updated.PublicKeyID = actor.PublicKeyID
		updated.PublicKeyPEM = actor.PublicKeyPEM
	}
	storedActor, err := h.repo.UpsertRemoteActor(ctx, updated)
	if err != nil {
		return "", fmt.Errorf("store updated actor: %w", err)
	}
	h.scheduleActorMedia(ctx, storedActor)
	return "ok: Person updated", nil
}

func (h *Handler) performUpdateQuestion(ctx context.Context, actor *actors.Actor, object map[string]any) (string, error) {
	if h.notes == nil || h.polls == nil {
		return "skip: poll repository is not configured", nil
	}
	uri, err := aptypes.GetAPID(object)
	if err != nil {
		return "skip: Question id is invalid", nil
	}
	note, err := h.notes.FindByURI(ctx, uri)
	if err != nil {
		return "", err
	}
	if note == nil {
		return "skip: Question is not registered", nil
	}
	attributedTo := actor.URI
	if object["attributedTo"] != nil {
		attributedTo, err = aptypes.GetOneAPID(object["attributedTo"])
	}
	if err != nil || attributedTo != actor.URI || note.AuthorID != actor.ID {
		return "skip: Question attribution mismatch", nil
	}
	existing, err := h.polls.FindByNoteID(ctx, note.ID)
	if err != nil {
		return "", err
	}
	if existing == nil {
		return "skip: Question is not registered", nil
	}
	votes, err := appolls.ParseUpdatedVotes(object, existing.Choices)
	if err != nil {
		return "skip: invalid Question update", nil
	}
	if _, err := h.polls.UpdateRemoteVotes(ctx, note.ID, actor.ID, votes); err != nil {
		return "", err
	}
	return "ok: Question updated", nil
}

func (h *Handler) updateObject(ctx context.Context, value any) (map[string]any, error) {
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	uri, err := aptypes.GetAPID(value)
	if err != nil {
		return nil, nil
	}
	if h.client == nil {
		return nil, fmt.Errorf("update object resolver is not configured")
	}
	object, err := h.client.FetchObject(ctx, uri, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve update object: %w", err)
	}
	return object, nil
}

func (h *Handler) CreateFollow(ctx context.Context, followerID, target string) (string, error) {
	if h.follows == nil || h.queue == nil {
		return "", fmt.Errorf("follow repository and queue are required")
	}
	follower, err := h.repo.FindLocalByID(ctx, followerID)
	if err != nil {
		return "", err
	}
	if follower == nil {
		return "", fmt.Errorf("local follower not found: %s", followerID)
	}

	var followee *actors.Actor
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		followee, err = h.resolver.ResolveActor(ctx, strings.TrimSpace(target))
	} else {
		followee, err = h.resolver.ResolveActorHandle(ctx, strings.TrimSpace(target))
	}
	if err != nil {
		return "", fmt.Errorf("resolve follow target: %w", err)
	}
	if followee == nil || followee.Host == nil {
		return "", fmt.Errorf("follow target must be a remote actor")
	}
	return h.enqueueOutgoingFollow(ctx, follower, followee)
}

func (h *Handler) enqueueOutgoingFollow(ctx context.Context, follower, followee *actors.Actor) (string, error) {
	if follower == nil || follower.Host != nil {
		return "", fmt.Errorf("follower must be a local actor")
	}
	if followee == nil || followee.Host == nil {
		return "", fmt.Errorf("follow target must be a remote actor")
	}
	blocked, err := h.isBlockedPair(ctx, follower.ID, followee.ID)
	if err != nil {
		return "", err
	}
	if blocked {
		return "", fmt.Errorf("follow target is blocked")
	}
	existing, err := h.follows.Find(ctx, follower.ID, followee.ID)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Status == follows.StatusAccepted {
		return "ok: already following", nil
	}

	remoteInbox := followee.Inbox
	isSharedInbox := false
	if remoteInbox == "" {
		remoteInbox = followee.SharedInbox
		isSharedInbox = true
	}
	if remoteInbox == "" {
		return "", fmt.Errorf("follow target inbox is empty")
	}
	activityID := strings.TrimRight(h.cfg.PublicURL, "/") + "/follows/" + url.PathEscape(follower.ID) + "/" + url.PathEscape(followee.ID)
	followActivity := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       activityID,
		"type":     "Follow",
		"actor":    follower.URI,
		"object":   followee.URI,
	}
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
		Status:              follows.StatusPending,
		RemoteActivityID:    activityID,
	}); err != nil {
		return "", err
	}
	task := queue.NewDeliverTask(follower.ID, remoteInbox, followActivity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if isSharedInbox {
		task = queue.NewSharedInboxDeliverTask(follower.ID, remoteInbox, followActivity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	}
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return "", err
	}
	return "ok: follow delivery enqueued", nil
}

func (h *Handler) performMove(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	targetURI := activityHref(activity["target"])
	if targetURI == "" {
		return "skip: invalid activity target", nil
	}
	if targetURI == actor.URI {
		return "skip: movedTo itself", nil
	}

	source, err := h.refreshRemoteActor(ctx, actor.URI)
	if err != nil {
		return "", fmt.Errorf("refresh move source: %w", err)
	}
	if source.MovedToURI != targetURI {
		return "skip: source movedTo does not match activity target", nil
	}
	destination, err := h.resolveMoveDestination(ctx, targetURI)
	if err != nil {
		return "", fmt.Errorf("resolve move destination: %w", err)
	}
	if destination == nil {
		return "skip: move destination not found", nil
	}
	if destination.MovedToURI == actor.URI {
		return "skip: circular move", nil
	}
	if !containsString(destination.AlsoKnownAs, actor.URI) {
		return "skip: destination alsoKnownAs does not include source", nil
	}

	now := time.Now().UTC()
	source.MovedAt = &now
	if _, err := h.repo.UpsertRemoteActor(ctx, *source); err != nil {
		return "", err
	}
	migrated, err := h.migrateLocalFollowers(ctx, source, destination)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ok: actor moved followers=%d", migrated), nil
}

func (h *Handler) refreshRemoteActor(ctx context.Context, uri string) (*actors.Actor, error) {
	if h.client == nil {
		return nil, fmt.Errorf("actor resolver is not configured")
	}
	object, err := h.client.FetchObject(ctx, uri, h.localActor)
	if err != nil {
		return nil, err
	}
	actor, err := apresolver.ParseRemoteActor(object, uri)
	if err != nil {
		return nil, err
	}
	return h.repo.UpsertRemoteActor(ctx, actor)
}

func (h *Handler) resolveMoveDestination(ctx context.Context, uri string) (*actors.Actor, error) {
	existing, err := h.repo.FindByURI(ctx, uri)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Host == nil {
		return existing, nil
	}
	return h.refreshRemoteActor(ctx, uri)
}

func (h *Handler) migrateLocalFollowers(ctx context.Context, source, destination *actors.Actor) (int, error) {
	if h.follows == nil {
		return 0, fmt.Errorf("follow repository is not configured")
	}
	const pageSize = 100
	migrated := 0
	afterID := ""
	for {
		page, err := h.follows.ListFollowersPage(ctx, source.ID, afterID, pageSize)
		if err != nil {
			return migrated, err
		}
		for _, oldFollow := range page {
			if oldFollow.FollowerHost != nil {
				continue
			}
			follower, err := h.repo.FindLocalByID(ctx, oldFollow.FollowerID)
			if err != nil {
				return migrated, err
			}
			if follower == nil {
				continue
			}
			if destination.Host == nil {
				if _, err := h.follows.Upsert(ctx, follows.Follow{
					FollowerID:  follower.ID,
					FolloweeID:  destination.ID,
					FollowerURI: follower.URI,
					FolloweeURI: destination.URI,
					Status:      follows.StatusAccepted,
					CreatedAt:   time.Now().UTC(),
				}); err != nil {
					return migrated, err
				}
			} else if _, err := h.enqueueOutgoingFollow(ctx, follower, destination); err != nil {
				return migrated, err
			}
			migrated++
		}
		if len(page) < pageSize {
			return migrated, nil
		}
		nextAfterID := page[len(page)-1].ID
		if nextAfterID == "" || nextAfterID == afterID {
			return migrated, fmt.Errorf("follow pagination did not advance")
		}
		afterID = nextAfterID
	}
}

func activityHref(value any) string {
	items := aptypes.ToArray(value)
	if len(items) == 0 {
		return ""
	}
	switch first := items[0].(type) {
	case string:
		return first
	case map[string]any:
		href, _ := first["href"].(string)
		return href
	default:
		return ""
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (h *Handler) DeleteFollow(ctx context.Context, command connector.FollowDeleteCommand) (connector.FollowDeleted, error) {
	if h.follows == nil || h.queue == nil {
		return connector.FollowDeleted{}, fmt.Errorf("follow repository and queue are required")
	}
	follower, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.FollowDeleted{}, err
	}
	if follower == nil {
		return connector.FollowDeleted{}, fmt.Errorf("local follower not found: %s", command.ActorID)
	}

	target := strings.TrimSpace(command.Target)
	var followee *actors.Actor
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		followee, err = h.resolver.ResolveActor(ctx, target)
	} else {
		followee, err = h.resolver.ResolveActorHandle(ctx, target)
	}
	if err != nil {
		return connector.FollowDeleted{}, fmt.Errorf("resolve follow target: %w", err)
	}
	if followee == nil || followee.Host == nil {
		return connector.FollowDeleted{}, fmt.Errorf("follow target must be a remote actor")
	}
	existing, err := h.follows.Find(ctx, follower.ID, followee.ID)
	if err != nil {
		return connector.FollowDeleted{}, err
	}
	if existing == nil {
		return connector.FollowDeleted{}, fmt.Errorf("follow relationship not found")
	}

	remoteInbox := strings.TrimSpace(followee.Inbox)
	isSharedInbox := false
	if remoteInbox == "" {
		remoteInbox = strings.TrimSpace(followee.SharedInbox)
		isSharedInbox = true
	}
	if remoteInbox == "" {
		return connector.FollowDeleted{}, fmt.Errorf("follow target inbox is empty")
	}
	followActivityID := existing.RemoteActivityID
	if followActivityID == "" {
		followActivityID = strings.TrimRight(h.cfg.PublicURL, "/") + "/follows/" + url.PathEscape(follower.ID) + "/" + url.PathEscape(followee.ID)
	}
	undoActivityID := strings.TrimRight(followActivityID, "/") + "/undo"
	followActivity := map[string]any{
		"id":     followActivityID,
		"type":   "Follow",
		"actor":  follower.URI,
		"object": followee.URI,
	}
	undoActivity := map[string]any{
		"@context":  "https://www.w3.org/ns/activitystreams",
		"id":        undoActivityID,
		"type":      "Undo",
		"actor":     follower.URI,
		"object":    followActivity,
		"published": time.Now().UTC().Format(time.RFC3339),
	}
	if err := h.follows.Delete(ctx, follower.ID, followee.ID, ""); err != nil {
		return connector.FollowDeleted{}, err
	}
	if err := h.refreshInstanceRelationshipCounts(ctx, followee.Host); err != nil {
		return connector.FollowDeleted{}, err
	}
	task := queue.NewDeliverTask(follower.ID, remoteInbox, undoActivity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if isSharedInbox {
		task = queue.NewSharedInboxDeliverTask(follower.ID, remoteInbox, undoActivity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	}
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return connector.FollowDeleted{}, fmt.Errorf("enqueue Undo(Follow) delivery: %w", err)
	}
	return connector.FollowDeleted{
		FollowerID: follower.ID,
		FolloweeID: followee.ID,
		URI:        undoActivityID,
	}, nil
}

func (h *Handler) performAcceptFollow(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	return h.finishOutgoingFollow(ctx, actor, activity, true)
}

func (h *Handler) performRejectFollow(ctx context.Context, actor *actors.Actor, activity map[string]any) (string, error) {
	return h.finishOutgoingFollow(ctx, actor, activity, false)
}

func (h *Handler) finishOutgoingFollow(ctx context.Context, actor *actors.Actor, activity map[string]any, accepted bool) (string, error) {
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	object, err := h.resolveOutgoingFollowObject(ctx, activity["object"])
	if err != nil {
		return "", err
	}
	if object == nil || !aptypes.IsFollow(object) {
		return "skip: accept/reject object is not a Follow", nil
	}
	followerURI, err := aptypes.GetAPID(object["actor"])
	if err != nil {
		return "skip: follow actor is invalid", nil
	}
	followeeURI, err := aptypes.GetAPID(object["object"])
	if err != nil || followeeURI != actor.URI {
		return "skip: follow object does not match accepting actor", nil
	}
	follower, err := h.repo.FindByURI(ctx, followerURI)
	if err != nil {
		return "", err
	}
	if follower == nil || follower.Host != nil {
		return "skip: follower is not a local actor", nil
	}
	follow, err := h.follows.Find(ctx, follower.ID, actor.ID)
	if err != nil {
		return "", err
	}
	if follow == nil || follow.Status != follows.StatusPending {
		return "skip: outgoing follow request is not pending", nil
	}
	if objectID, _ := object["id"].(string); objectID != "" && follow.RemoteActivityID != "" && objectID != follow.RemoteActivityID {
		return "skip: follow activity id mismatch", nil
	}
	if accepted {
		if _, err := h.follows.Approve(ctx, follower.ID, actor.ID); err != nil {
			return "", err
		}
		if err := h.refreshInstanceRelationshipCounts(ctx, actor.Host); err != nil {
			return "", err
		}
		return "ok: outgoing follow accepted", nil
	}
	activityID, _ := activity["id"].(string)
	if err := h.follows.Delete(ctx, follower.ID, actor.ID, activityID); err != nil {
		return "", err
	}
	if err := h.refreshInstanceRelationshipCounts(ctx, actor.Host); err != nil {
		return "", err
	}
	return "ok: outgoing follow rejected", nil
}

func (h *Handler) resolveOutgoingFollowObject(ctx context.Context, value any) (map[string]any, error) {
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	uri, err := aptypes.GetAPID(value)
	if err != nil {
		return nil, nil
	}
	follow, err := h.follows.FindByRemoteActivityID(ctx, uri)
	if err != nil {
		return nil, err
	}
	if follow != nil {
		return map[string]any{
			"id":     uri,
			"type":   "Follow",
			"actor":  follow.FollowerURI,
			"object": follow.FolloweeURI,
		}, nil
	}
	if h.client == nil || h.cfg.IsSelfFederationURL(uri) {
		return nil, nil
	}
	object, err := h.client.FetchObject(ctx, uri, h.localActor)
	if err != nil {
		return nil, fmt.Errorf("resolve accept/reject object: %w", err)
	}
	return object, nil
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
		if existing.AuthorID != actor.ID || existing.AttributedTo != actor.URI {
			return "skip: existing note belongs to a different actor", nil
		}
		if err := h.storeRemotePoll(ctx, actor, existing, object); err != nil {
			return "", err
		}
		h.scheduleNoteMedia(ctx, existing)
		var reply *domainnotes.Note
		if existing.ReplyID != "" {
			reply, err = h.notes.FindByID(ctx, existing.ReplyID)
			if err != nil {
				return "", err
			}
		}
		activityID, _ := activity["id"].(string)
		if err := h.createNoteNotifications(ctx, actor, existing, reply, activityID); err != nil {
			return "", err
		}
		return "skip: note exists", nil
	}
	parsed, err := apnotes.ParseRemoteNote(object, uri)
	if err != nil {
		return fmt.Sprintf("skip: invalid note: %v", err), nil
	}
	if err := h.upsertRemoteEmojis(ctx, actor, parsed.Emojis); err != nil && h.logger != nil {
		h.logger.Printf("activitypub: store Create emoji tags: %v", err)
	}
	reply, quote, err := h.resolver.ResolveNoteLinks(ctx, parsed.URI, parsed.InReplyToURI, parsed.QuoteURI)
	if err != nil {
		return "", err
	}
	if handled, result, err := h.performRemotePollVote(ctx, actor, object, reply); handled {
		return result, err
	}
	note := domainnotes.Note{
		URI:             parsed.URI,
		AttributedTo:    parsed.AttributedTo,
		AuthorID:        actor.ID,
		Text:            parsed.Text,
		ContentWarning:  parsed.ContentWarning,
		Sensitive:       parsed.Sensitive,
		InReplyToURI:    parsed.InReplyToURI,
		QuoteURI:        parsed.QuoteURI,
		Visibility:      domainnotes.Visibility(parsed.Visibility),
		MentionURIs:     parsed.MentionURIs,
		VisibleUserURIs: parsed.VisibleUserURIs,
		Hashtags:        parsed.Hashtags,
		Emojis:          parsed.Emojis,
		Attachments:     parsed.Attachments,
		Raw:             object,
		CreatedAt:       time.Now().UTC(),
		PublishedAt:     publishedAt(object),
	}
	if reply != nil {
		note.ReplyID = reply.ID
	}
	if quote != nil {
		note.QuoteID = quote.ID
	}
	stored, err := h.notes.UpsertRemoteNote(ctx, note)
	if err != nil {
		return "", err
	}
	if err := h.storeRemotePoll(ctx, actor, stored, object); err != nil {
		return "", err
	}
	h.scheduleNoteMedia(ctx, stored)
	activityID, _ := activity["id"].(string)
	if err := h.createNoteNotifications(ctx, actor, stored, reply, activityID); err != nil {
		return "", err
	}
	return "ok: note created", nil
}

func (h *Handler) storeRemotePoll(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, object map[string]any) error {
	if h.polls == nil || actor == nil || note == nil || !aptypes.IsType(object, "Question") || !aptypes.IsType(note.Raw, "Question") {
		return nil
	}
	poll, err := appolls.ParseQuestion(object)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("activitypub: ignore invalid Question uri=%s: %v", note.URI, err)
		}
		return nil
	}
	poll.NoteID = note.ID
	poll.AuthorID = actor.ID
	poll.AuthorHost = actor.Host
	_, err = h.polls.UpsertRemote(ctx, *poll)
	return err
}

func (h *Handler) scheduleActorMedia(ctx context.Context, actor *actors.Actor) {
	if actor == nil {
		return
	}
	for _, rawURL := range []string{actor.AvatarURL, actor.BannerURL} {
		if err := h.ScheduleMedia(ctx, rawURL); err != nil && h.logger != nil {
			h.logger.Printf("media: schedule actor source=%s: %v", rawURL, err)
		}
	}
}

func (h *Handler) scheduleNoteMedia(ctx context.Context, note *domainnotes.Note) {
	if note == nil {
		return
	}
	for _, attachment := range note.Attachments {
		if err := h.ScheduleMedia(ctx, attachment.URL); err != nil && h.logger != nil {
			h.logger.Printf("media: schedule note attachment=%s: %v", attachment.URL, err)
		}
	}
	for _, emoji := range note.Emojis {
		if err := h.ScheduleMedia(ctx, emoji.IconURL); err != nil && h.logger != nil {
			h.logger.Printf("media: schedule note emoji=%s: %v", emoji.IconURL, err)
		}
	}
}

func (h *Handler) performRemotePollVote(ctx context.Context, actor *actors.Actor, object map[string]any, reply *domainnotes.Note) (bool, string, error) {
	name, _ := object["name"].(string)
	name = strings.TrimSpace(name)
	if h.polls == nil || reply == nil || name == "" {
		return false, "", nil
	}
	poll, err := h.polls.FindByNoteID(ctx, reply.ID)
	if err != nil {
		return true, "", err
	}
	if poll == nil {
		return false, "", nil
	}
	choice := -1
	for i, value := range poll.Choices {
		if value == name {
			choice = i
			break
		}
	}
	if choice < 0 {
		return true, "skip: poll choice not found", nil
	}
	allowed, err := h.canReactToNote(ctx, actor, reply)
	if err != nil {
		return true, "", err
	}
	if !allowed {
		return true, "skip: poll is not visible to actor", nil
	}
	_, updated, err := h.polls.RecordVote(ctx, reply.ID, actor.ID, choice, time.Now().UTC())
	alreadyVoted := errors.Is(err, polls.ErrAlreadyVoted)
	if errors.Is(err, polls.ErrExpired) {
		return true, "skip: poll expired", nil
	}
	if err != nil && !alreadyVoted {
		return true, "", err
	}
	if updated.AuthorHost == nil {
		owner, err := h.repo.FindLocalByID(ctx, updated.AuthorID)
		if err != nil {
			return true, "", err
		}
		if owner != nil {
			activity := apnotes.RenderQuestionUpdate(reply, updated, time.Now().UTC())
			if err := h.enqueueNoteActivityDeliveries(ctx, owner, reply, activity); err != nil {
				return true, "", err
			}
		}
	}
	if alreadyVoted {
		return true, "skip: already voted", nil
	}
	return true, "ok: poll vote created", nil
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
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	blocked, err := h.isBlockedPair(ctx, follower.ID, followee.ID)
	if err != nil {
		return "", err
	}
	if blocked {
		return "skip: follow is blocked", nil
	}
	activityID, _ := activity["id"].(string)
	follow, err := h.follows.Upsert(ctx, follows.Follow{
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
		Status:              follows.StatusPending,
		RemoteActivityID:    activityID,
	})
	if err != nil {
		return "", err
	}
	if err := h.createNotification(ctx, followee, notifications.KindFollowRequest, follower, "", activityID); err != nil {
		return "", err
	}
	if h.connector != nil {
		if err := h.connector.PublishFollowApprovalRequested(ctx, connector.FollowApproval{
			AccountID:   followee.OwnerAccountID,
			FollowerID:  follow.FollowerID,
			FolloweeID:  follow.FolloweeID,
			FollowerURI: follow.FollowerURI,
			FolloweeURI: follow.FolloweeURI,
		}); err != nil {
			return "", err
		}
	}
	return "ok: follow request pending", nil
}

func (h *Handler) ApproveFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	if h.queue == nil {
		return "skip: queue is not configured", nil
	}
	blocked, err := h.isBlockedPair(ctx, followerID, followeeID)
	if err != nil {
		return "", err
	}
	if blocked {
		return "skip: follow is blocked", nil
	}
	follow, err := h.follows.Approve(ctx, followerID, followeeID)
	if err != nil {
		return "", err
	}
	if follow == nil {
		return "skip: follow request not found", nil
	}
	if err := h.refreshInstanceRelationshipCounts(ctx, follow.FollowerHost, follow.FolloweeHost); err != nil {
		return "", err
	}
	followee, err := h.repo.FindLocalByID(ctx, followeeID)
	if err != nil {
		return "", err
	}
	if followee == nil {
		return "skip: followee is not a local user", nil
	}
	inbox := follow.FollowerInbox
	isSharedInbox := false
	if inbox == "" {
		inbox = follow.FollowerSharedInbox
		isSharedInbox = true
	}
	if inbox == "" {
		return "skip: follower inbox is empty", nil
	}
	followActivity := map[string]any{
		"type":   "Follow",
		"actor":  follow.FollowerURI,
		"object": follow.FolloweeURI,
	}
	if follow.RemoteActivityID != "" {
		followActivity["id"] = follow.RemoteActivityID
	}
	accept := renderAccept(followee, followActivity)
	task := queue.NewDeliverTask(followee.ID, inbox, accept, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if isSharedInbox {
		task = queue.NewSharedInboxDeliverTask(followee.ID, inbox, accept, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	}
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return "", err
	}
	if h.connector != nil {
		if err := h.connector.PublishFollowApprovalCompleted(ctx, connector.FollowApproval{
			AccountID:   followee.OwnerAccountID,
			FollowerID:  follow.FollowerID,
			FolloweeID:  follow.FolloweeID,
			FollowerURI: follow.FollowerURI,
			FolloweeURI: follow.FolloweeURI,
		}); err != nil {
			return "", err
		}
	}
	return "ok: follow accepted delivery enqueued", nil
}

func (h *Handler) RejectFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	if h.queue == nil {
		return "skip: queue is not configured", nil
	}
	follow, err := h.follows.Find(ctx, followerID, followeeID)
	if err != nil {
		return "", err
	}
	if follow == nil {
		return "skip: follow request not found", nil
	}
	if follow.Status != follows.StatusPending {
		return "skip: follow request is not pending", nil
	}
	followee, err := h.repo.FindLocalByID(ctx, followeeID)
	if err != nil {
		return "", err
	}
	if followee == nil {
		return "skip: followee is not a local user", nil
	}
	inbox := follow.FollowerInbox
	isSharedInbox := false
	if inbox == "" {
		inbox = follow.FollowerSharedInbox
		isSharedInbox = true
	}
	if inbox == "" {
		return "skip: follower inbox is empty", nil
	}
	followActivity := map[string]any{
		"type":   "Follow",
		"actor":  follow.FollowerURI,
		"object": follow.FolloweeURI,
	}
	if follow.RemoteActivityID != "" {
		followActivity["id"] = follow.RemoteActivityID
	}
	if err := h.follows.Delete(ctx, followerID, followeeID, ""); err != nil {
		return "", err
	}
	if err := h.refreshInstanceRelationshipCounts(ctx, follow.FollowerHost, follow.FolloweeHost); err != nil {
		return "", err
	}
	reject := renderReject(followee, followActivity)
	task := queue.NewDeliverTask(followee.ID, inbox, reject, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if isSharedInbox {
		task = queue.NewSharedInboxDeliverTask(followee.ID, inbox, reject, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	}
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return "", err
	}
	if h.connector != nil {
		if err := h.connector.PublishFollowApprovalRejected(ctx, connector.FollowApproval{
			AccountID:   followee.OwnerAccountID,
			FollowerID:  follow.FollowerID,
			FolloweeID:  follow.FolloweeID,
			FollowerURI: follow.FollowerURI,
			FolloweeURI: follow.FolloweeURI,
		}); err != nil {
			return "", err
		}
	}
	return "ok: follow rejected delivery enqueued", nil
}

func (h *Handler) CreatePost(ctx context.Context, command connector.PostCreateCommand) (connector.PostCreated, error) {
	if h.notes == nil {
		return connector.PostCreated{}, fmt.Errorf("note repository is not configured")
	}
	actor, err := h.repo.FindLocalByID(ctx, command.ActorID)
	if err != nil {
		return connector.PostCreated{}, err
	}
	if actor == nil {
		return connector.PostCreated{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	var localPoll *polls.Poll
	if command.Poll != nil {
		if h.polls == nil {
			return connector.PostCreated{}, fmt.Errorf("poll repository is not configured")
		}
		localPoll, err = appolls.NewLocalPoll(command.Poll.Choices, command.Poll.Multiple, command.Poll.ExpiresAt)
		if err != nil {
			return connector.PostCreated{}, err
		}
	}
	visibility, err := postVisibility(command.Visibility)
	if err != nil {
		return connector.PostCreated{}, err
	}
	mentionURIs := command.MentionURIs
	var specifiedRecipients []*actors.Actor
	if visibility == domainnotes.VisibilitySpecified {
		specifiedRecipients, mentionURIs, err = h.resolveSpecifiedRecipients(ctx, actor, command.MentionURIs)
		if err != nil {
			return connector.PostCreated{}, err
		}
	} else if len(mentionURIs) > 0 {
		_, mentionURIs, err = h.resolveDirectRecipients(ctx, actor, mentionURIs)
		if err != nil {
			return connector.PostCreated{}, err
		}
	}
	now := time.Now().UTC()
	localEmojis, err := h.resolveLocalEmojis(ctx, command.EmojiNames)
	if err != nil {
		return connector.PostCreated{}, err
	}
	noteURI := strings.TrimRight(h.cfg.PublicURL, "/") + "/notes/" + url.PathEscape(command.NoteID)
	visibleUserURIs := []string(nil)
	if visibility == domainnotes.VisibilitySpecified {
		visibleUserURIs = mentionURIs
	}
	note, err := h.notes.CreateLocalNote(ctx, domainnotes.Note{
		ID:              command.NoteID,
		URI:             noteURI,
		AttributedTo:    actor.URI,
		AuthorID:        actor.ID,
		Text:            command.Text,
		ContentWarning:  command.ContentWarning,
		Sensitive:       command.Sensitive,
		InReplyToURI:    command.InReplyToURI,
		QuoteURI:        command.QuoteURI,
		Visibility:      visibility,
		MentionURIs:     mentionURIs,
		VisibleUserURIs: visibleUserURIs,
		Hashtags:        command.Hashtags,
		Emojis:          localEmojis,
		CreatedAt:       now,
		PublishedAt:     &now,
	})
	if err != nil {
		return connector.PostCreated{}, err
	}
	if localPoll != nil {
		localPoll.NoteID = note.ID
		localPoll.AuthorID = actor.ID
		localPoll.AuthorHost = nil
		localPoll.CreatedAt = now
		localPoll.UpdatedAt = now
		localPoll, err = h.polls.UpsertLocal(ctx, *localPoll)
		if err != nil {
			return connector.PostCreated{}, err
		}
		if localPoll.ExpiresAt != nil {
			if h.queue == nil {
				return connector.PostCreated{}, fmt.Errorf("queue is required for poll expiration")
			}
			if err := h.queue.Enqueue(ctx, queue.NewPollEndedTask(note.ID, localPoll.ExpiresAt.Sub(now))); err != nil {
				return connector.PostCreated{}, err
			}
		}
	}
	if note.Visibility == domainnotes.VisibilitySpecified {
		if err := h.enqueueSpecifiedCreateNoteDeliveries(ctx, actor, note, localPoll, specifiedRecipients); err != nil {
			return connector.PostCreated{}, err
		}
	} else {
		if err := h.enqueueCreateNoteDeliveries(ctx, actor, note, localPoll); err != nil {
			return connector.PostCreated{}, err
		}
	}
	payload := connector.PostCreated{
		AccountID: actor.OwnerAccountID,
		ActorID:   actor.ID,
		NoteID:    note.ID,
		URI:       note.URI,
	}
	if h.connector != nil {
		if err := h.connector.PublishPostCreated(ctx, payload); err != nil {
			return connector.PostCreated{}, err
		}
	}
	return payload, nil
}

func (h *Handler) resolveLocalEmojis(ctx context.Context, names []string) ([]domainnotes.Emoji, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if len(names) > 100 {
		return nil, fmt.Errorf("at most 100 custom emojis are allowed")
	}
	for _, name := range names {
		if !validLocalEmojiName(strings.Trim(strings.TrimSpace(name), ":")) {
			return nil, fmt.Errorf("invalid local custom emoji name: %q", name)
		}
	}
	if h.emojis == nil {
		return nil, fmt.Errorf("emoji repository is not configured")
	}
	stored, err := h.emojis.FindLocalByNames(ctx, names)
	if err != nil {
		return nil, err
	}
	result := make([]domainnotes.Emoji, 0, len(stored))
	for _, emoji := range stored {
		iconURL := emoji.PublicURL
		if iconURL == "" {
			iconURL = emoji.OriginalURL
		}
		if iconURL == "" {
			continue
		}
		updatedAt := emoji.UpdatedAt
		result = append(result, domainnotes.Emoji{
			Name: emoji.Name, URI: emoji.URI, IconURL: iconURL,
			MediaType: emoji.MediaType, UpdatedAt: &updatedAt,
		})
	}
	return result, nil
}

func validLocalEmojiName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func (h *Handler) VotePoll(ctx context.Context, command connector.PollVoteCommand) (connector.PollVoted, error) {
	if h.notes == nil || h.polls == nil {
		return connector.PollVoted{}, fmt.Errorf("note and poll repositories are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.PollVoted{}, err
	}
	if actor == nil {
		return connector.PollVoted{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.PollVoted{}, err
	}
	if note == nil {
		return connector.PollVoted{}, fmt.Errorf("poll note not found: %s", command.NoteID)
	}
	allowed, err := h.canReactToNote(ctx, actor, note)
	if err != nil {
		return connector.PollVoted{}, err
	}
	if !allowed {
		return connector.PollVoted{}, fmt.Errorf("poll is not visible to actor")
	}
	now := time.Now().UTC()
	vote, poll, err := h.polls.RecordVote(ctx, note.ID, actor.ID, command.Choice, now)
	if err != nil && !errors.Is(err, polls.ErrAlreadyVoted) {
		return connector.PollVoted{}, err
	}
	result := connector.PollVoted{VoteID: vote.ID, NoteID: note.ID, Choice: vote.Choice}
	if poll.AuthorHost != nil {
		if h.queue == nil {
			return connector.PollVoted{}, fmt.Errorf("queue is required for remote poll vote")
		}
		owner, err := h.repo.FindByURI(ctx, note.AttributedTo)
		if err != nil {
			return connector.PollVoted{}, err
		}
		if owner == nil || owner.Host == nil || strings.TrimSpace(owner.Inbox) == "" {
			return connector.PollVoted{}, fmt.Errorf("remote poll owner inbox is unavailable")
		}
		activity := appolls.RenderVote(actor.URI, vote, note, poll, now)
		if err := h.queue.Enqueue(ctx, queue.NewDeliverTask(actor.ID, owner.Inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)); err != nil {
			return connector.PollVoted{}, err
		}
		result.URI, _ = activity["id"].(string)
	} else {
		owner, err := h.repo.FindLocalByID(ctx, poll.AuthorID)
		if err != nil {
			return connector.PollVoted{}, err
		}
		if owner == nil {
			return connector.PollVoted{}, fmt.Errorf("local poll owner is unavailable")
		}
		activity := apnotes.RenderQuestionUpdate(note, poll, now)
		if err := h.enqueueNoteActivityDeliveries(ctx, owner, note, activity); err != nil {
			return connector.PollVoted{}, err
		}
	}
	return result, nil
}

func (h *Handler) DeletePost(ctx context.Context, command connector.PostDeleteCommand) (connector.PostDeleted, error) {
	if h.notes == nil || h.queue == nil {
		return connector.PostDeleted{}, fmt.Errorf("note repository and queue are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.PostDeleted{}, err
	}
	if actor == nil {
		return connector.PostDeleted{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindAnyByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.PostDeleted{}, err
	}
	if note == nil || note.AuthorID != actor.ID || note.AttributedTo != actor.URI {
		return connector.PostDeleted{}, fmt.Errorf("owned local note not found: %s", command.NoteID)
	}
	deletedAt := time.Now().UTC()
	if note.DeletedAt == nil {
		if err := h.notes.DeleteLocalNote(ctx, note.ID, actor.ID); err != nil {
			return connector.PostDeleted{}, err
		}
	} else {
		deletedAt = note.DeletedAt.UTC()
	}
	if err := h.cleanupNote(ctx, note.ID); err != nil {
		return connector.PostDeleted{}, err
	}
	activity := apnotes.RenderDelete(note, deletedAt)
	if err := h.enqueueNoteActivityDeliveries(ctx, actor, note, activity); err != nil {
		return connector.PostDeleted{}, err
	}
	return connector.PostDeleted{ActorID: actor.ID, NoteID: note.ID, URI: note.URI}, nil
}

func (h *Handler) CreateReaction(ctx context.Context, command connector.ReactionCreateCommand) (connector.ReactionCreated, error) {
	if h.notes == nil || h.reactions == nil || h.queue == nil {
		return connector.ReactionCreated{}, fmt.Errorf("note repository, reaction repository, and queue are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if actor == nil {
		return connector.ReactionCreated{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if note == nil {
		return connector.ReactionCreated{}, fmt.Errorf("note not found: %s", command.NoteID)
	}
	allowed, err := h.canReactToNote(ctx, actor, note)
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if !allowed {
		return connector.ReactionCreated{}, fmt.Errorf("note is not visible to actor")
	}
	reactionValue := strings.TrimSpace(command.Reaction)
	if reactionValue == "" {
		return connector.ReactionCreated{}, fmt.Errorf("reaction is required")
	}
	recipient, err := h.repo.FindByURI(ctx, note.AttributedTo)
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	if recipient == nil || recipient.Host == nil {
		return connector.ReactionCreated{}, fmt.Errorf("reaction target author is not remote")
	}
	inbox := strings.TrimSpace(recipient.Inbox)
	if inbox == "" {
		return connector.ReactionCreated{}, fmt.Errorf("reaction target inbox is empty")
	}
	stored, err := h.reactions.Upsert(ctx, reactions.Reaction{
		NoteID:    note.ID,
		NoteURI:   note.URI,
		ActorID:   actor.ID,
		ActorURI:  actor.URI,
		ActorHost: actor.Host,
		Reaction:  reactionValue,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return connector.ReactionCreated{}, err
	}
	activity := apreactions.RenderLike(h.cfg.PublicURL, stored)
	task := queue.NewDeliverTask(actor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return connector.ReactionCreated{}, fmt.Errorf("enqueue Like delivery: %w", err)
	}
	return connector.ReactionCreated{
		ReactionID: stored.ID,
		NoteID:     stored.NoteID,
		Reaction:   stored.Reaction,
		URI:        activity["id"].(string),
	}, nil
}

func (h *Handler) DeleteReaction(ctx context.Context, command connector.ReactionDeleteCommand) (connector.ReactionDeleted, error) {
	if h.notes == nil || h.reactions == nil || h.queue == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("note repository, reaction repository, and queue are required")
	}
	actor, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if actor == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("local actor not found: %s", command.ActorID)
	}
	note, err := h.notes.FindByID(ctx, strings.TrimSpace(command.NoteID))
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if note == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("note not found: %s", command.NoteID)
	}
	existing, err := h.reactions.Find(ctx, note.ID, actor.ID)
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if existing == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("reaction not found")
	}
	recipient, err := h.repo.FindByURI(ctx, note.AttributedTo)
	if err != nil {
		return connector.ReactionDeleted{}, err
	}
	if recipient == nil || recipient.Host == nil {
		return connector.ReactionDeleted{}, fmt.Errorf("reaction target author is not remote")
	}
	inbox := strings.TrimSpace(recipient.Inbox)
	if inbox == "" {
		return connector.ReactionDeleted{}, fmt.Errorf("reaction target inbox is empty")
	}
	undo := apreactions.RenderUndoLike(h.cfg.PublicURL, existing, time.Now().UTC())
	if err := h.reactions.Delete(ctx, note.ID, actor.ID, ""); err != nil {
		return connector.ReactionDeleted{}, err
	}
	task := queue.NewDeliverTask(actor.ID, inbox, undo, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return connector.ReactionDeleted{}, fmt.Errorf("enqueue Undo(Like) delivery: %w", err)
	}
	return connector.ReactionDeleted{
		ReactionID: existing.ID,
		NoteID:     existing.NoteID,
		URI:        undo["id"].(string),
	}, nil
}

func (h *Handler) canReactToNote(ctx context.Context, actor *actors.Actor, note *domainnotes.Note) (bool, error) {
	blocked, err := h.isBlockedPair(ctx, actor.ID, note.AuthorID)
	if err != nil || blocked {
		return false, err
	}
	switch note.Visibility {
	case domainnotes.VisibilityPublic, domainnotes.VisibilityHome:
		return true, nil
	case domainnotes.VisibilityFollowers:
		if h.follows == nil {
			return false, nil
		}
		follow, err := h.follows.Find(ctx, actor.ID, note.AuthorID)
		return err == nil && follow != nil && follow.Status == follows.StatusAccepted, err
	case domainnotes.VisibilitySpecified:
		audience := note.VisibleUserURIs
		if len(audience) == 0 {
			audience = note.MentionURIs
		}
		for _, uri := range audience {
			if uri == actor.URI {
				return true, nil
			}
		}
	}
	return false, nil
}

func (h *Handler) resolveSpecifiedRecipients(ctx context.Context, actor *actors.Actor, mentionURIs []string) ([]*actors.Actor, []string, error) {
	recipients, uris, err := h.resolveDirectRecipients(ctx, actor, mentionURIs)
	if err != nil {
		return nil, nil, err
	}
	if len(recipients) == 0 {
		return nil, nil, fmt.Errorf("specified visibility requires at least one mention_uri")
	}
	return recipients, uris, nil
}

func (h *Handler) resolveDirectRecipients(ctx context.Context, actor *actors.Actor, mentionURIs []string) ([]*actors.Actor, []string, error) {
	if h.resolver == nil {
		return nil, nil, fmt.Errorf("actor resolver is not configured")
	}
	recipients := make([]*actors.Actor, 0, len(mentionURIs))
	uris := make([]string, 0, len(mentionURIs))
	seen := make(map[string]struct{}, len(mentionURIs))
	for _, rawURI := range mentionURIs {
		uri := strings.TrimSpace(rawURI)
		if uri == "" {
			continue
		}
		if _, exists := seen[uri]; exists {
			continue
		}
		recipient, err := h.resolver.ResolveActor(ctx, uri)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve specified recipient %s: %w", uri, err)
		}
		blocked, err := h.isBlockedPair(ctx, actor.ID, recipient.ID)
		if err != nil {
			return nil, nil, err
		}
		if blocked {
			continue
		}
		seen[uri] = struct{}{}
		uris = append(uris, uri)
		recipients = append(recipients, recipient)
	}
	return recipients, uris, nil
}

func (h *Handler) enqueueSpecifiedCreateNoteDeliveries(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, poll *polls.Poll, recipients []*actors.Actor) error {
	if h.queue == nil {
		return fmt.Errorf("queue is required for specified post delivery")
	}
	activity := apnotes.RenderCreateWithPoll(note, poll)
	destinations := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		if recipient == nil || recipient.Host == nil {
			continue
		}
		if h.cfg.IsFederationHostBlocked(*recipient.Host) {
			continue
		}
		blocked, err := h.isBlockedPair(ctx, actor.ID, recipient.ID)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		inbox := strings.TrimSpace(recipient.Inbox)
		if inbox == "" {
			return fmt.Errorf("specified recipient inbox is empty: %s", recipient.URI)
		}
		if _, exists := destinations[inbox]; exists {
			continue
		}
		destinations[inbox] = struct{}{}
		task := queue.NewDeliverTask(actor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
		if err := h.queue.Enqueue(ctx, task); err != nil {
			return fmt.Errorf("enqueue specified Create(Note) delivery to %s: %w", inbox, err)
		}
	}
	return nil
}

func (h *Handler) enqueueCreateNoteDeliveries(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, poll *polls.Poll) error {
	return h.enqueueNoteActivityDeliveries(ctx, actor, note, apnotes.RenderCreateWithPoll(note, poll))
}

func (h *Handler) enqueueNoteActivityDeliveries(ctx context.Context, actor *actors.Actor, note *domainnotes.Note, activity map[string]any) error {
	if h.follows == nil || h.queue == nil {
		return fmt.Errorf("follow repository and queue are required for post delivery")
	}
	destinations := make(map[string]struct{})
	recipientIDs := make(map[string]struct{})
	if note.Visibility != domainnotes.VisibilitySpecified {
		afterID := ""
		for {
			followers, err := h.follows.ListFollowersPage(ctx, actor.ID, afterID, postDeliveryFollowerLimit)
			if err != nil {
				return fmt.Errorf("list followers for post delivery: %w", err)
			}
			remoteIDs := make([]string, 0, len(followers))
			for _, follow := range followers {
				if follow.FollowerHost != nil {
					remoteIDs = append(remoteIDs, follow.FollowerID)
				}
			}
			activeRemoteIDs, err := h.repo.FilterActiveRemoteIDs(ctx, remoteIDs)
			if err != nil {
				return fmt.Errorf("filter active post recipients: %w", err)
			}
			for _, follow := range followers {
				if follow.FollowerHost == nil {
					continue
				}
				if _, active := activeRemoteIDs[follow.FollowerID]; !active {
					continue
				}
				if h.cfg.IsFederationHostBlocked(*follow.FollowerHost) {
					continue
				}
				blocked, err := h.isBlockedPair(ctx, actor.ID, follow.FollowerID)
				if err != nil {
					return err
				}
				if blocked {
					continue
				}
				inbox := strings.TrimSpace(follow.FollowerSharedInbox)
				isSharedInbox := inbox != ""
				if inbox == "" {
					inbox = strings.TrimSpace(follow.FollowerInbox)
				}
				if err := h.enqueueNoteActivity(ctx, actor.ID, inbox, activity, destinations, isSharedInbox); err != nil {
					return err
				}
				recipientIDs[follow.FollowerID] = struct{}{}
			}
			if len(followers) < postDeliveryFollowerLimit {
				break
			}
			afterID = followers[len(followers)-1].ID
		}
	}
	directURIs := note.MentionURIs
	if note.Visibility == domainnotes.VisibilitySpecified && len(note.VisibleUserURIs) > 0 {
		directURIs = note.VisibleUserURIs
	}
	for _, uri := range directURIs {
		recipient, err := h.resolver.ResolveActor(ctx, uri)
		if err != nil {
			return fmt.Errorf("resolve direct post recipient %s: %w", uri, err)
		}
		if recipient == nil || recipient.Host == nil {
			continue
		}
		if _, exists := recipientIDs[recipient.ID]; exists {
			continue
		}
		if h.cfg.IsFederationHostBlocked(*recipient.Host) {
			continue
		}
		blocked, err := h.isBlockedPair(ctx, actor.ID, recipient.ID)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		if err := h.enqueueNoteActivity(ctx, actor.ID, strings.TrimSpace(recipient.Inbox), activity, destinations, false); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) enqueueNoteActivity(ctx context.Context, actorID, inbox string, activity map[string]any, destinations map[string]struct{}, isSharedInbox bool) error {
	if inbox == "" {
		return nil
	}
	if _, exists := destinations[inbox]; exists {
		return nil
	}
	destinations[inbox] = struct{}{}
	task := queue.NewDeliverTask(actorID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	if isSharedInbox {
		task = queue.NewSharedInboxDeliverTask(actorID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
	}
	if err := h.queue.Enqueue(ctx, task); err != nil {
		return fmt.Errorf("enqueue note activity delivery to %s: %w", inbox, err)
	}
	return nil
}

func (h *Handler) CreateActor(ctx context.Context, accountID string, command connector.ActorCreateCommand) (connector.ActorCreated, error) {
	accountID = strings.TrimSpace(accountID)
	username := strings.TrimSpace(command.Username)
	if accountID == "" {
		return connector.ActorCreated{}, fmt.Errorf("owner account id is required")
	}
	if !validLocalActorUsername(username) {
		return connector.ActorCreated{}, fmt.Errorf("invalid local actor username")
	}
	actorType := strings.TrimSpace(command.Type)
	if actorType == "" {
		actorType = "Person"
	}
	if !validLocalActorType(actorType) {
		return connector.ActorCreated{}, fmt.Errorf("invalid local actor type")
	}
	id, err := newLocalActorID()
	if err != nil {
		return connector.ActorCreated{}, err
	}
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	uri := base + "/users/" + url.PathEscape(id)
	name := strings.TrimSpace(command.Name)
	if name == "" {
		name = username
	}
	actor, err := h.repo.CreateOwnedLocalActor(ctx, actors.Actor{
		ID:             id,
		OwnerAccountID: accountID,
		Username:       username,
		UsernameLower:  strings.ToLower(username),
		Name:           name,
		Type:           actorType,
		URI:            uri,
		Inbox:          uri + "/inbox",
		SharedInbox:    base + "/inbox",
		FollowersURI:   uri + "/followers",
		FollowingURI:   uri + "/following",
		FeaturedURI:    uri + "/collections/featured",
		PublicKeyID:    uri + "#main-key",
	})
	if err != nil {
		return connector.ActorCreated{}, err
	}
	return connector.ActorCreated{ActorID: actor.ID, URI: actor.URI, Username: actor.Username}, nil
}

func newLocalActorID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate local actor id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func validLocalActorType(value string) bool {
	switch value {
	case "Person", "Service", "Application", "Group", "Organization":
		return true
	default:
		return false
	}
}

func validLocalActorUsername(username string) bool {
	if username == "" || len(username) > 128 {
		return false
	}
	for i, r := range username {
		ok := r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !ok || ((i == 0 || i == len(username)-1) && (r == '-' || r == '.')) {
			return false
		}
	}
	return true
}

func postVisibility(value string) (domainnotes.Visibility, error) {
	switch domainnotes.Visibility(strings.TrimSpace(value)) {
	case "":
		return domainnotes.VisibilityPublic, nil
	case domainnotes.VisibilityPublic:
		return domainnotes.VisibilityPublic, nil
	case domainnotes.VisibilityHome:
		return domainnotes.VisibilityHome, nil
	case domainnotes.VisibilityFollowers:
		return domainnotes.VisibilityFollowers, nil
	case domainnotes.VisibilitySpecified:
		return domainnotes.VisibilitySpecified, nil
	default:
		return "", fmt.Errorf("unsupported post visibility: %s", value)
	}
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
	if h.follows != nil {
		if err := h.follows.Delete(ctx, blocker.ID, blockee.ID, ""); err != nil {
			return "", err
		}
		if err := h.follows.Delete(ctx, blockee.ID, blocker.ID, ""); err != nil {
			return "", err
		}
		if err := h.refreshInstanceRelationshipCounts(ctx, blocker.Host, blockee.Host); err != nil {
			return "", err
		}
	}
	return "ok", nil
}

func (h *Handler) performFlag(ctx context.Context, reporter *actors.Actor, activity map[string]any) (string, error) {
	if h.reports == nil {
		return "skip: report repository is not configured", nil
	}
	objectURIs := aptypes.GetAPIDs(activity["object"])
	target, err := h.firstLocalFlagTarget(ctx, objectURIs)
	if err != nil {
		return "", err
	}
	if target == nil {
		return "skip", nil
	}
	activityID, _ := activity["id"].(string)
	content, _ := activity["content"].(string)
	if _, err := h.reports.Create(ctx, reports.Report{
		TargetUserID:     target.ID,
		TargetUserHost:   target.Host,
		ReporterID:       reporter.ID,
		ReporterHost:     reporter.Host,
		ReporterURI:      reporter.URI,
		Content:          content,
		Comment:          flagComment(content, objectURIs),
		ObjectURIs:       objectURIs,
		RemoteActivityID: activityID,
		CreatedAt:        time.Now().UTC(),
	}); err != nil {
		return "", err
	}
	return "ok", nil
}

func (h *Handler) firstLocalFlagTarget(ctx context.Context, objectURIs []string) (*actors.Actor, error) {
	for _, uri := range objectURIs {
		if !strings.HasPrefix(uri, strings.TrimRight(h.cfg.PublicURL, "/")+"/users/") {
			continue
		}
		target, err := h.repo.FindByURI(ctx, uri)
		if err != nil {
			return nil, err
		}
		if target != nil && target.Host == nil {
			return target, nil
		}
	}
	return nil, nil
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
	blocked, err := h.isBlockedPair(ctx, actor.ID, note.AuthorID)
	if err != nil {
		return "", err
	}
	if blocked {
		return "skip: reaction is blocked", nil
	}
	reaction := reactionFromActivity(activity)
	if err := h.upsertRemoteEmojis(ctx, actor, apnotes.ExtractEmojis(activity["tag"])); err != nil && h.logger != nil {
		h.logger.Printf("activitypub: store reaction emoji tags: %v", err)
	}
	existing, err := h.reactions.Find(ctx, note.ID, actor.ID)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Reaction == reaction {
		activityID, _ := activity["id"].(string)
		recipient, findErr := h.repo.FindLocalByID(ctx, note.AuthorID)
		if findErr != nil {
			return "", findErr
		}
		if err := h.createNotification(ctx, recipient, notifications.KindReaction, actor, note.ID, activityID); err != nil {
			return "", err
		}
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
	recipient, err := h.repo.FindLocalByID(ctx, note.AuthorID)
	if err != nil {
		return "", err
	}
	if err := h.createNotification(ctx, recipient, notifications.KindReaction, actor, note.ID, activityID); err != nil {
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
		if existing.AuthorID == actor.ID && existing.RenoteID != "" {
			target, findErr := h.notes.FindByID(ctx, existing.RenoteID)
			if findErr != nil {
				return "", findErr
			}
			if target != nil {
				recipient, findErr := h.repo.FindLocalByID(ctx, target.AuthorID)
				if findErr != nil {
					return "", findErr
				}
				if err := h.createNotification(ctx, recipient, notifications.KindRenote, actor, target.ID, activityID); err != nil {
					return "", err
				}
			}
		}
		return "skip: announce exists", nil
	}
	targetURI, err := aptypes.GetAPID(activity["object"])
	if err != nil {
		return "skip: announce target is invalid", nil
	}
	targetHost, err := hostOf(targetURI)
	if err != nil {
		return "skip: announce target is invalid", nil
	}
	if h.cfg.IsFederationHostBlocked(targetHost) {
		return "skip: announce target host is blocked", nil
	}
	target, err := h.resolveAnnounceTarget(ctx, targetURI)
	if err != nil {
		return "", err
	}
	if target == nil {
		return fmt.Sprintf("skip: announce target not found %s", targetURI), nil
	}
	blocked, err := h.isBlockedPair(ctx, actor.ID, target.AuthorID)
	if err != nil {
		return "", err
	}
	if blocked {
		return "skip: announce is blocked", nil
	}
	if target.RenoteID != "" && target.Text == "" && target.InReplyToURI == "" && len(target.Attachments) == 0 {
		return "skip: cannot announce a pure Announce", nil
	}
	if !canAnnounceNote(actor, target) {
		return "skip: announce target is not shareable", nil
	}
	announcedAt := publishedAt(activity)
	if announcedAt != nil && target.PublishedAt != nil && announcedAt.Before(*target.PublishedAt) {
		return "skip: malformed announce published timestamp", nil
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
		PublishedAt:  announcedAt,
	}
	_, err = h.notes.UpsertRemoteNote(ctx, note)
	if err != nil {
		return "", err
	}
	recipient, err := h.repo.FindLocalByID(ctx, target.AuthorID)
	if err != nil {
		return "", err
	}
	if err := h.createNotification(ctx, recipient, notifications.KindRenote, actor, target.ID, activityID); err != nil {
		return "", err
	}
	return "ok: announce created", nil
}

func canAnnounceNote(actor *actors.Actor, note *domainnotes.Note) bool {
	if actor == nil || note == nil {
		return false
	}
	switch note.Visibility {
	case domainnotes.VisibilityPublic, domainnotes.VisibilityHome:
		return true
	case domainnotes.VisibilityFollowers:
		return actor.ID == note.AuthorID
	case domainnotes.VisibilitySpecified:
		return false
	default:
		return false
	}
}

func (h *Handler) upsertRemoteEmojis(ctx context.Context, actor *actors.Actor, values []domainnotes.Emoji) error {
	if h.emojis == nil || actor == nil || actor.Host == nil {
		return nil
	}
	for _, value := range values {
		if _, err := h.emojis.UpsertRemote(ctx, emojis.Emoji{
			Host: *actor.Host, Name: value.Name, URI: value.URI,
			OriginalURL: value.IconURL, MediaType: value.MediaType,
			RemoteUpdatedAt: value.UpdatedAt,
		}); err != nil {
			return fmt.Errorf("upsert remote emoji %s@%s: %w", value.Name, *actor.Host, err)
		}
		if err := h.ScheduleMedia(ctx, value.IconURL); err != nil && h.logger != nil {
			h.logger.Printf("activitypub: schedule emoji media url=%s error=%v", value.IconURL, err)
		}
	}
	return nil
}

func (h *Handler) createNoteNotifications(ctx context.Context, source *actors.Actor, note, reply *domainnotes.Note, activityID string) error {
	if note != nil && note.URI != "" {
		activityID = note.URI
	}
	seen := map[string]struct{}{}
	if reply != nil {
		recipient, err := h.repo.FindLocalByID(ctx, reply.AuthorID)
		if err != nil {
			return err
		}
		if recipient != nil {
			if err := h.createNotification(ctx, recipient, notifications.KindReply, source, note.ID, activityID); err != nil {
				return err
			}
			seen[recipient.ID] = struct{}{}
		}
	}
	for _, uri := range note.MentionURIs {
		recipient, err := h.repo.FindByURI(ctx, uri)
		if err != nil {
			return err
		}
		if recipient == nil || recipient.Host != nil || recipient.IsSuspended {
			continue
		}
		if _, exists := seen[recipient.ID]; exists {
			continue
		}
		if err := h.createNotification(ctx, recipient, notifications.KindMention, source, note.ID, activityID); err != nil {
			return err
		}
		seen[recipient.ID] = struct{}{}
	}
	return nil
}

func (h *Handler) createNotification(ctx context.Context, recipient *actors.Actor, kind string, source *actors.Actor, noteID, activityID string) error {
	if h.notifications == nil || recipient == nil || recipient.Host != nil || recipient.OwnerAccountID == "" || recipient.IsSuspended || source == nil {
		return nil
	}
	notification, err := h.notifications.Upsert(ctx, notifications.Notification{
		RecipientAccountID: recipient.OwnerAccountID,
		RecipientActorID:   recipient.ID,
		Kind:               kind,
		SourceActorID:      source.ID,
		NoteID:             noteID,
		RemoteActivityID:   activityID,
		CreatedAt:          time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	if h.connector != nil {
		return h.connector.PublishNotificationCreated(ctx, connector.NotificationCreated{
			AccountID:        notification.RecipientAccountID,
			RecipientActorID: notification.RecipientActorID,
			NotificationID:   notification.ID,
			Kind:             notification.Kind,
			SourceActorID:    notification.SourceActorID,
			NoteID:           notification.NoteID,
		})
	}
	return nil
}

func (h *Handler) isBlockedPair(ctx context.Context, firstID, secondID string) (bool, error) {
	if h.blocks == nil || firstID == "" || secondID == "" || firstID == secondID {
		return false, nil
	}
	for _, pair := range [][2]string{{firstID, secondID}, {secondID, firstID}} {
		block, err := h.blocks.Find(ctx, pair[0], pair[1])
		if err != nil {
			return false, err
		}
		if block != nil {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) resolveAnnounceTarget(ctx context.Context, targetURI string) (*domainnotes.Note, error) {
	target, err := h.resolver.ResolveNote(ctx, targetURI)
	if err != nil {
		return nil, fmt.Errorf("resolve announce target: %w", err)
	}
	return target, nil
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
	if aptypes.IsAccept(object) {
		return h.performUndoAccept(ctx, actor, activity, object)
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
	if err := h.refreshInstanceRelationshipCounts(ctx, actor.Host); err != nil {
		return "", err
	}
	return "ok: unfollowed", nil
}

func (h *Handler) performUndoAccept(ctx context.Context, actor *actors.Actor, activity, accept map[string]any) (string, error) {
	acceptActorURI, err := aptypes.GetAPID(accept["actor"])
	if err != nil {
		return "skip: undo accept actor is invalid", nil
	}
	if acceptActorURI != actor.URI {
		return "skip: undo accept actor mismatch", nil
	}

	object := accept["object"]
	var followerURI string
	if follow, ok := object.(map[string]any); ok && aptypes.IsFollow(follow) {
		followerURI, err = aptypes.GetAPID(follow["actor"])
		if err != nil {
			return "skip: accepted follow actor is invalid", nil
		}
		followeeURI, followeeErr := aptypes.GetAPID(follow["object"])
		if followeeErr != nil || followeeURI != actor.URI {
			return "skip: accepted follow object mismatch", nil
		}
	} else {
		followerURI, err = aptypes.GetAPID(object)
		if err != nil {
			return "skip: accepted follower is invalid", nil
		}
	}
	follower, err := h.repo.FindByURI(ctx, followerURI)
	if err != nil {
		return "", err
	}
	if follower == nil || follower.Host != nil {
		return "skip: accepted follower is not a local user", nil
	}
	if h.follows == nil {
		return "skip: follow repository is not configured", nil
	}
	follow, err := h.follows.Find(ctx, follower.ID, actor.ID)
	if err != nil {
		return "", err
	}
	if follow == nil || follow.Status != follows.StatusAccepted {
		return "skip: not following", nil
	}
	undoID, _ := activity["id"].(string)
	if err := h.follows.Delete(ctx, follower.ID, actor.ID, undoID); err != nil {
		return "", err
	}
	if err := h.refreshInstanceRelationshipCounts(ctx, actor.Host); err != nil {
		return "", err
	}
	return "ok: unfollowed", nil
}

func (h *Handler) refreshInstanceRelationshipCounts(ctx context.Context, hosts ...*string) error {
	if h.instances == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if host == nil || strings.TrimSpace(*host) == "" {
			continue
		}
		normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*host), "."))
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		if _, err := h.instances.RefreshRelationshipCounts(ctx, normalized, time.Now().UTC()); err != nil {
			return fmt.Errorf("refresh instance relationship counts for %s: %w", normalized, err)
		}
	}
	return nil
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
	note, err := h.notes.FindAnyByURI(ctx, announceURI)
	if err != nil {
		return "", err
	}
	if note == nil || note.AuthorID != actor.ID {
		return "skip: no such Announce", nil
	}
	if err := h.notes.DeleteRemoteNote(ctx, announceURI, actor.ID); err != nil {
		return "", err
	}
	if err := h.cleanupNote(ctx, note.ID); err != nil {
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
			return h.performDeleteActor(ctx, actor, uri)
		}
		return fmt.Sprintf("skip: unknown delete object type %s", formerType), nil
	}
	if h.notes == nil {
		return "skip: note repository is not configured", nil
	}
	note, err := h.notes.FindAnyByURI(ctx, uri)
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
	if err := h.cleanupNote(ctx, note.ID); err != nil {
		return "", err
	}
	return "ok: note deleted", nil
}

func (h *Handler) cleanupNote(ctx context.Context, noteID string) error {
	if h.cleanup == nil {
		return nil
	}
	result, err := h.cleanup.CleanupNote(ctx, noteID)
	if err != nil {
		return err
	}
	if h.logger != nil && (result.Reactions != 0 || result.Polls != 0 || result.PollVotes != 0 || result.Notifications != 0) {
		h.logger.Printf("note-delete: cleaned note=%s reactions=%d polls=%d poll_votes=%d notifications=%d", noteID, result.Reactions, result.Polls, result.PollVotes, result.Notifications)
	}
	return nil
}

func (h *Handler) performDeleteActor(ctx context.Context, actor *actors.Actor, uri string) (string, error) {
	if actor.URI != uri {
		return fmt.Sprintf("skip: delete actor %s !== %s", actor.URI, uri), nil
	}
	if h.queue == nil {
		return "skip: queue is not configured", nil
	}
	if err := h.repo.MarkRemoteActorDeleted(ctx, uri); err != nil {
		return "", err
	}
	if err := h.queue.Enqueue(ctx, queue.NewAccountDeleteTask(actor.ID, actor.URI)); err != nil {
		return "", err
	}
	return "ok: account delete queued", nil
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

func flagComment(content string, objectURIs []string) string {
	raw, err := json.MarshalIndent(objectURIs, "", "  ")
	if err != nil {
		raw = []byte("[]")
	}
	return content + "\n" + string(raw)
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

func renderReject(localActor *actors.Actor, follow map[string]any) map[string]any {
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":     strings.TrimRight(localActor.URI, "/") + "#rejects/" + shortID(follow["id"]),
		"type":   "Reject",
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

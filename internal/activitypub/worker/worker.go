package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	apactors "github.com/nexryai/rosmarinus/internal/activitypub/actors"
	appolls "github.com/nexryai/rosmarinus/internal/activitypub/polls"
	apresolver "github.com/nexryai/rosmarinus/internal/activitypub/resolver"
	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	apwebfinger "github.com/nexryai/rosmarinus/internal/activitypub/webfinger"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/activities"
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
	remoteFeaturedNoteLimit   = 5
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
	cfg              config.Config
	logger           *log.Logger
	repo             actors.Repository
	notes            domainnotes.Repository
	follows          follows.Repository
	blocks           blocks.Repository
	emojis           emojis.Repository
	reactions        reactions.Repository
	reports          reports.Repository
	notifications    notifications.Repository
	polls            polls.Repository
	cleanup          cleanup.Repository
	media            domainmedia.Repository
	mediaFetcher     MediaFetcher
	instances        instances.Repository
	metadataFetcher  InstanceMetadataFetcher
	queue            QueueClient
	client           APClient
	connector        ConnectorPublisher
	locker           ActivityLocker
	resolver         *apresolver.Resolver
	localActor       *actors.Actor
	activityReceipts activities.ReceiptRepository
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

func (h *Handler) SetWebFingerResolver(webFinger apresolver.WebFinger) {
	h.resolver.SetWebFinger(webFinger)
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

func (h *Handler) SetActivityReceiptRepository(repository activities.ReceiptRepository) {
	h.activityReceipts = repository
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
	publicBase := strings.TrimRight(h.cfg.PublicURL, "/") + "/media"
	mediaRecord, err := h.media.UpsertPending(ctx, target.String(), publicBase)
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
	if actor.ID != payload.ActorID || !actor.IsSuspended || actor.DeletedAt == nil {
		return fmt.Errorf("account delete task actor does not match a deleted actor")
	}
	if payload.Local {
		if actor.Host != nil {
			return fmt.Errorf("local account delete task actor is remote")
		}
	} else if actor.Host == nil {
		return fmt.Errorf("remote account delete task actor is local")
	}
	result, err := h.cleanup.CleanupActor(ctx, actor.ID)
	if err != nil {
		return err
	}
	if h.logger != nil {
		h.logger.Printf("account-delete: cleaned actor=%s local=%t notes=%d reactions=%d follows=%d blocks=%d polls=%d notifications=%d", actor.ID, payload.Local, result.Notes, result.Reactions, result.Follows, result.Blocks, result.Polls, result.Notifications)
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
	if payload.Version != 1 {
		return fmt.Errorf("unsupported deliver task payload version: %d", payload.Version)
	}
	actor, err := h.repo.FindLocalForDeliveryByID(ctx, payload.ActorID)
	if err != nil {
		return err
	}
	if actor == nil {
		return fmt.Errorf("deliver actor not found: %s", payload.ActorID)
	}
	if actor.IsSuspended {
		activityID, idErr := aptypes.GetAPID(payload.Object)
		objectURI, objectErr := aptypes.GetAPID(payload.Object["object"])
		expectedDeleteID := actor.URI + "#delete"
		if actor.DeletedAt == nil && actor.SuspendedAt != nil {
			expectedDeleteID = apactors.SuspensionDeleteID(actor.URI, actor.SuspendedAt.UTC())
		}
		if aptypes.IsDelete(payload.Object) && idErr == nil && activityID == expectedDeleteID && objectErr == nil && objectURI == actor.URI {
			// Lifecycle Delete is the sole activity that may use a suspended
			// Actor's retained signing key.
		} else if actor.DeletedAt == nil && actor.SuspendedAt != nil && isMatchingUnsuspension(payload.Object, actor.URI, expectedDeleteID) {
			return fmt.Errorf("deliver actor has not resumed yet: %s", payload.ActorID)
		} else {
			return fmt.Errorf("deliver actor is suspended: %s: %w", payload.ActorID, asynq.SkipRetry)
		}
	} else if activityID, idErr := aptypes.GetAPID(payload.Object); aptypes.IsDelete(payload.Object) && idErr == nil && strings.HasPrefix(activityID, actor.URI+"#suspensions/") {
		// A suspension Delete queued just before the atomic state transition
		// retries until the repository exposes the matching suspended state.
		return fmt.Errorf("deliver actor suspension is not active: %s", payload.ActorID)
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

func isMatchingUnsuspension(activity map[string]any, actorURI, deleteID string) bool {
	if !aptypes.IsUndo(activity) {
		return false
	}
	activityActor, err := aptypes.GetAPID(activity["actor"])
	if err != nil || activityActor != actorURI {
		return false
	}
	object, ok := activity["object"].(map[string]any)
	if !ok || !aptypes.IsDelete(object) {
		return false
	}
	objectID, err := aptypes.GetAPID(object)
	if err != nil || objectID != deleteID {
		return false
	}
	deletedActor, err := aptypes.GetAPID(object["object"])
	return err == nil && deletedActor == actorURI
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
	signerAuthority, err := aptypes.Authority(authActor.URI)
	if err != nil {
		return "skip: signer uri host is invalid", nil
	}
	activityAuthority, err := aptypes.Authority(activityID)
	if err != nil || signerAuthority != activityAuthority {
		return fmt.Sprintf("skip: signerHost(%s) != activity.id host(%s)", signerAuthority, activityAuthority), nil
	}
	if h.instances != nil {
		signerHost, _ := hostOf(authActor.URI)
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
	var receiptClaim *activities.Claim
	if h.activityReceipts != nil {
		lease := h.cfg.InboxQueue.Timeout + time.Minute
		claim, claimed, err := h.activityReceipts.Claim(ctx, activityID, authActor.URI, time.Now().UTC(), lease, h.cfg.InboxActivityReceiptTTL)
		if err != nil {
			return "", fmt.Errorf("claim activity receipt: %w", err)
		}
		if !claimed {
			return "skip: activity was already processed or is in progress", nil
		}
		receiptClaim = claim
	}
	result, performErr := h.performActivity(ctx, authActor, payload.Activity)
	if receiptClaim == nil {
		return result, performErr
	}
	if performErr != nil {
		h.releaseActivityReceipt(*receiptClaim)
		return result, performErr
	}
	if err := h.activityReceipts.Complete(ctx, *receiptClaim, time.Now().UTC()); err != nil {
		// Complete can fail before MongoDB applies the transition. A token-scoped
		// release makes that case retryable without deleting a completed receipt
		// when the response was lost after a successful write.
		h.releaseActivityReceipt(*receiptClaim)
		return "", fmt.Errorf("complete activity receipt: %w", err)
	}
	return result, nil
}

func (h *Handler) releaseActivityReceipt(claim activities.Claim) {
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.activityReceipts.Release(releaseCtx, claim); err != nil && h.logger != nil {
		h.logger.Printf("inbox: release activity receipt id=%s err=%v", claim.ActivityID, err)
	}
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
	actorAuthority, err := aptypes.Authority(actor.URI)
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
		activityAuthority, err := aptypes.Authority(activityID)
		if err != nil || activityAuthority != actorAuthority {
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
	case aptypes.IsAdd(activity):
		return h.performFeaturedChange(ctx, actor, activity, true)
	case aptypes.IsRemove(activity):
		return h.performFeaturedChange(ctx, actor, activity, false)
	case aptypes.IsMove(activity):
		return h.performMove(ctx, actor, activity)
	default:
		return fmt.Sprintf("skip: unrecognized activity type %v", activity["type"]), nil
	}
}

func (h *Handler) performFeaturedChange(ctx context.Context, actor *actors.Actor, activity map[string]any, add bool) (string, error) {
	if actor == nil || actor.Host == nil {
		return "skip: featured change actor is not remote", nil
	}
	activityActorURI, err := aptypes.GetAPID(activity["actor"])
	if err != nil || activityActorURI != actor.URI {
		return "skip: featured change actor mismatch", nil
	}
	targetURI, err := aptypes.GetAPID(activity["target"])
	if err != nil {
		return "skip: featured change target is invalid", nil
	}
	if actor.FeaturedURI == "" || targetURI != actor.FeaturedURI {
		return "skip: featured change target is not actor featured collection", nil
	}
	noteURI, err := aptypes.GetAPID(activity["object"])
	if err != nil {
		return "skip: featured change object is invalid", nil
	}
	if h.resolver == nil || h.notes == nil {
		return "skip: note resolver is not configured", nil
	}
	note, err := h.resolver.ResolveNote(ctx, noteURI)
	if err != nil {
		return "", fmt.Errorf("resolve featured note: %w", err)
	}
	// Misskey only permits an Actor to pin its own Notes. Enforcing ownership
	// here prevents a valid signer from mutating another Actor's profile state.
	if note == nil || note.AuthorID != actor.ID || note.AttributedTo != actor.URI {
		return "skip: featured note attribution mismatch", nil
	}
	if add {
		if _, err := h.repo.AddRemoteFeaturedNote(ctx, actor.URI, note.ID, remoteFeaturedNoteLimit); err != nil {
			return "", fmt.Errorf("add remote featured note: %w", err)
		}
		return "ok: featured note added", nil
	}
	if _, err := h.repo.RemoveRemoteFeaturedNote(ctx, actor.URI, note.ID); err != nil {
		return "", fmt.Errorf("remove remote featured note: %w", err)
	}
	return "ok: featured note removed", nil
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

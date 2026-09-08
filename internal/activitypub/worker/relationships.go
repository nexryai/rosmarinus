package worker

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	apblocks "github.com/nexryai/rosmarinus/internal/activitypub/blocks"
	apresolver "github.com/nexryai/rosmarinus/internal/activitypub/resolver"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/queue"
)

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

	followee, err := h.ResolveRemoteActor(ctx, target)
	if err != nil {
		return "", fmt.Errorf("resolve follow target: %w", err)
	}
	return h.enqueueOutgoingFollow(ctx, follower, followee)
}

func (h *Handler) ResolveRemoteActor(ctx context.Context, target string) (*actors.Actor, error) {
	target = strings.TrimSpace(target)
	var (
		actor *actors.Actor
		err   error
	)
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		actor, err = h.resolver.ResolveActor(ctx, target)
	} else {
		actor, err = h.resolver.ResolveActorHandle(ctx, strings.TrimPrefix(target, "@"))
	}
	if err != nil {
		return nil, err
	}
	if actor == nil || actor.Host == nil {
		return nil, fmt.Errorf("target must be a remote actor")
	}
	return actor, nil
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

func (h *Handler) CreateBlock(ctx context.Context, command connector.BlockCreateCommand) (connector.BlockCreated, error) {
	if h.blocks == nil || h.queue == nil {
		return connector.BlockCreated{}, fmt.Errorf("block repository and queue are required")
	}
	blocker, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.BlockCreated{}, err
	}
	if blocker == nil {
		return connector.BlockCreated{}, fmt.Errorf("local blocker not found: %s", command.ActorID)
	}
	blockee, err := h.resolveRemoteActorTarget(ctx, command.Target)
	if err != nil {
		return connector.BlockCreated{}, fmt.Errorf("resolve block target: %w", err)
	}
	inbox, isSharedInbox, err := deliveryInbox(blockee)
	if err != nil {
		return connector.BlockCreated{}, fmt.Errorf("block target %w", err)
	}
	blockers, err := h.accountBlockers(ctx, blocker)
	if err != nil {
		return connector.BlockCreated{}, err
	}
	var result connector.BlockCreated
	for i := range blockers {
		accountActor := &blockers[i]
		stored, err := h.blocks.Upsert(ctx, blocks.Block{
			BlockerID:   accountActor.ID,
			BlockeeID:   blockee.ID,
			BlockerURI:  accountActor.URI,
			BlockeeURI:  blockee.URI,
			BlockerHost: accountActor.Host,
			BlockeeHost: blockee.Host,
			CreatedAt:   time.Now().UTC(),
		})
		if err != nil {
			return connector.BlockCreated{}, err
		}
		if h.follows != nil {
			if err := h.follows.Delete(ctx, accountActor.ID, blockee.ID, ""); err != nil {
				return connector.BlockCreated{}, err
			}
			if err := h.follows.Delete(ctx, blockee.ID, accountActor.ID, ""); err != nil {
				return connector.BlockCreated{}, err
			}
		}
		activity := apblocks.RenderBlock(h.cfg.PublicURL, stored)
		created := connector.BlockCreated{BlockID: stored.ID, BlockeeID: stored.BlockeeID, URI: activity["id"].(string)}
		if accountActor.ID == blocker.ID {
			result = created
		}
		if accountActor.IsSuspended {
			continue
		}
		task := queue.NewDeliverTask(accountActor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
		if isSharedInbox {
			task = queue.NewSharedInboxDeliverTask(accountActor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
		}
		if err := h.queue.Enqueue(ctx, task); err != nil {
			return connector.BlockCreated{}, fmt.Errorf("enqueue Block delivery for actor %s: %w", accountActor.ID, err)
		}
	}
	if h.follows != nil {
		if err := h.refreshInstanceRelationshipCounts(ctx, blockee.Host); err != nil {
			return connector.BlockCreated{}, err
		}
	}
	if result.BlockID == "" {
		return connector.BlockCreated{}, fmt.Errorf("initiating actor is missing from its owning account")
	}
	return result, nil
}

func (h *Handler) DeleteBlock(ctx context.Context, command connector.BlockDeleteCommand) (connector.BlockDeleted, error) {
	if h.blocks == nil || h.queue == nil {
		return connector.BlockDeleted{}, fmt.Errorf("block repository and queue are required")
	}
	blocker, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.BlockDeleted{}, err
	}
	if blocker == nil {
		return connector.BlockDeleted{}, fmt.Errorf("local blocker not found: %s", command.ActorID)
	}
	blockee, err := h.resolveRemoteActorTarget(ctx, command.Target)
	if err != nil {
		return connector.BlockDeleted{}, fmt.Errorf("resolve block target: %w", err)
	}
	inbox, isSharedInbox, err := deliveryInbox(blockee)
	if err != nil {
		return connector.BlockDeleted{}, fmt.Errorf("block target %w", err)
	}
	blockers, err := h.accountBlockers(ctx, blocker)
	if err != nil {
		return connector.BlockDeleted{}, err
	}
	var result connector.BlockDeleted
	deleted := 0
	deletedAt := time.Now().UTC()
	for i := range blockers {
		accountActor := &blockers[i]
		existing, err := h.blocks.Find(ctx, accountActor.ID, blockee.ID)
		if err != nil {
			return connector.BlockDeleted{}, err
		}
		if existing == nil {
			continue
		}
		activity := apblocks.RenderUndoBlock(h.cfg.PublicURL, existing, deletedAt)
		if err := h.blocks.Delete(ctx, accountActor.ID, blockee.ID, activity["id"].(string)); err != nil {
			return connector.BlockDeleted{}, err
		}
		deleted++
		current := connector.BlockDeleted{BlockID: existing.ID, BlockeeID: existing.BlockeeID, URI: activity["id"].(string)}
		if result.BlockID == "" || accountActor.ID == blocker.ID {
			result = current
		}
		if accountActor.IsSuspended {
			continue
		}
		task := queue.NewDeliverTask(accountActor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
		if isSharedInbox {
			task = queue.NewSharedInboxDeliverTask(accountActor.ID, inbox, activity, h.cfg.DeliverQueue.MaxRetry, h.cfg.DeliverQueue.Timeout)
		}
		if err := h.queue.Enqueue(ctx, task); err != nil {
			return connector.BlockDeleted{}, fmt.Errorf("enqueue Undo(Block) delivery for actor %s: %w", accountActor.ID, err)
		}
	}
	if deleted == 0 {
		return connector.BlockDeleted{}, fmt.Errorf("block relationship not found")
	}
	return result, nil
}

func (h *Handler) accountBlockers(ctx context.Context, initiating *actors.Actor) ([]actors.Actor, error) {
	if initiating == nil {
		return nil, fmt.Errorf("initiating blocker is missing")
	}
	if initiating.OwnerAccountID == "" || initiating.IsSystemActor {
		return []actors.Actor{*initiating}, nil
	}
	result := make([]actors.Actor, 0, 1)
	afterID := ""
	for {
		page, err := h.repo.ListOwnedLocalActorsPage(ctx, initiating.OwnerAccountID, afterID, postDeliveryFollowerLimit, false)
		if err != nil {
			return nil, fmt.Errorf("list account actors for block: %w", err)
		}
		result = append(result, page...)
		if len(page) < postDeliveryFollowerLimit {
			break
		}
		next := page[len(page)-1].ID
		if next == "" || next <= afterID {
			return nil, fmt.Errorf("account actor pagination did not advance")
		}
		afterID = next
	}
	return result, nil
}

func (h *Handler) resolveRemoteActorTarget(ctx context.Context, target string) (*actors.Actor, error) {
	target = strings.TrimSpace(target)
	var (
		actor *actors.Actor
		err   error
	)
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		actor, err = h.resolver.ResolveActor(ctx, target)
	} else {
		actor, err = h.resolver.ResolveActorHandle(ctx, target)
	}
	if err != nil {
		return nil, err
	}
	if actor == nil || actor.Host == nil {
		return nil, fmt.Errorf("target must be a remote actor")
	}
	return actor, nil
}

func deliveryInbox(actor *actors.Actor) (string, bool, error) {
	if actor == nil {
		return "", false, fmt.Errorf("actor is missing")
	}
	if inbox := strings.TrimSpace(actor.Inbox); inbox != "" {
		return inbox, false, nil
	}
	if inbox := strings.TrimSpace(actor.SharedInbox); inbox != "" {
		return inbox, true, nil
	}
	return "", false, fmt.Errorf("inbox is empty")
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

func (h *Handler) RejectAndBlockFollow(ctx context.Context, followerID, followeeID string) (connector.BlockCreated, error) {
	if h.follows == nil || h.blocks == nil || h.queue == nil {
		return connector.BlockCreated{}, fmt.Errorf("follow repository, block repository, and queue are required")
	}
	follow, err := h.follows.Find(ctx, strings.TrimSpace(followerID), strings.TrimSpace(followeeID))
	if err != nil {
		return connector.BlockCreated{}, err
	}
	if follow == nil {
		return connector.BlockCreated{}, fmt.Errorf("follow request not found")
	}
	if follow.Status != follows.StatusPending {
		return connector.BlockCreated{}, fmt.Errorf("follow request is not pending")
	}
	follower, err := h.repo.FindAnyByID(ctx, follow.FollowerID)
	if err != nil {
		return connector.BlockCreated{}, err
	}
	if follower == nil || follower.Host == nil || follower.URI != follow.FollowerURI {
		return connector.BlockCreated{}, fmt.Errorf("follow requester is not a matching remote actor")
	}
	result, err := h.RejectFollow(ctx, follow.FollowerID, follow.FolloweeID)
	if err != nil {
		return connector.BlockCreated{}, err
	}
	if !strings.HasPrefix(result, "ok:") {
		return connector.BlockCreated{}, fmt.Errorf("reject follow request: %s", result)
	}
	created, err := h.CreateBlock(ctx, connector.BlockCreateCommand{ActorID: follow.FolloweeID, Target: follower.URI})
	if err != nil {
		return connector.BlockCreated{}, fmt.Errorf("block rejected follower: %w", err)
	}
	return created, nil
}

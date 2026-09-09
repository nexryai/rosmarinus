package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/domain/reports"
	"github.com/nexryai/rosmarinus/internal/queue"
)

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
	reactionEmojis := apnotes.ExtractEmojis(activity["tag"])
	reactionEmoji := matchingReactionEmoji(reaction, reactionEmojis)
	if err := h.upsertRemoteEmojis(ctx, actor, reactionEmojis); err != nil && h.logger != nil {
		h.logger.Printf("activitypub: store reaction emoji tags: %v", err)
	}
	existing, err := h.reactions.Find(ctx, note.ID, actor.ID)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Reaction == reaction {
		if reactionEmoji != nil && existing.EmojiURL != reactionEmoji.IconURL {
			existing.EmojiName = reactionEmoji.Name
			existing.EmojiURL = reactionEmoji.IconURL
			existing.EmojiMediaType = reactionEmoji.MediaType
			if _, err := h.reactions.Upsert(ctx, *existing); err != nil {
				return "", err
			}
		}
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
	reactionRecord := reactions.Reaction{
		NoteID:           note.ID,
		NoteURI:          note.URI,
		ActorID:          actor.ID,
		ActorURI:         actor.URI,
		ActorHost:        actor.Host,
		Reaction:         reaction,
		RemoteActivityID: activityID,
		CreatedAt:        time.Now().UTC(),
	}
	if reactionEmoji != nil {
		reactionRecord.EmojiName = reactionEmoji.Name
		reactionRecord.EmojiURL = reactionEmoji.IconURL
		reactionRecord.EmojiMediaType = reactionEmoji.MediaType
	}
	if _, err := h.reactions.Upsert(ctx, reactionRecord); err != nil {
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
	stored, err := h.notes.UpsertRemoteNote(ctx, note)
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
	if err := h.publishRemoteNoteCreated(ctx, actor, stored); err != nil && h.logger != nil {
		h.logger.Printf("realtime: publish remote announce created uri=%s: %v", stored.URI, err)
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

func matchingReactionEmoji(reaction string, values []domainnotes.Emoji) *domainnotes.Emoji {
	if len(reaction) < 3 || reaction[0] != ':' || reaction[len(reaction)-1] != ':' {
		return nil
	}
	name := reaction[1 : len(reaction)-1]
	if index := strings.LastIndexByte(name, '@'); index >= 0 {
		name = name[:index]
	}
	for index := range values {
		if values[index].Name == name && values[index].IconURL != "" {
			return &values[index]
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

func (h *Handler) publishRemoteNoteCreated(ctx context.Context, author *actors.Actor, note *domainnotes.Note) error {
	if h.connector == nil || author == nil || author.Host == nil || note == nil {
		return nil
	}
	publish := func(accountID, actorID string) error {
		if strings.TrimSpace(accountID) == "" {
			return nil
		}
		return h.connector.PublishPostCreated(ctx, connector.PostCreated{
			AccountID: accountID,
			ActorID:   actorID,
			NoteID:    note.ID,
			URI:       note.URI,
		})
	}
	if note.Visibility == domainnotes.VisibilityPublic {
		accountIDs, err := h.repo.ListOwnedAccountIDs(ctx)
		if err != nil {
			return fmt.Errorf("list accounts for remote public note event: %w", err)
		}
		seen := make(map[string]struct{}, len(accountIDs))
		for _, accountID := range accountIDs {
			accountID = strings.TrimSpace(accountID)
			if accountID == "" {
				continue
			}
			if _, exists := seen[accountID]; exists {
				continue
			}
			seen[accountID] = struct{}{}
			// An empty Actor ID invalidates the public timeline for every selected
			// Actor owned by this account.
			if err := publish(accountID, ""); err != nil {
				return err
			}
		}
		return nil
	}

	recipients := make(map[string]*actors.Actor)
	if note.Visibility == domainnotes.VisibilityHome || note.Visibility == domainnotes.VisibilityFollowers {
		if h.follows == nil {
			return fmt.Errorf("follow repository is not configured")
		}
		afterID := ""
		for {
			page, err := h.follows.ListFollowersPage(ctx, author.ID, afterID, postDeliveryFollowerLimit)
			if err != nil {
				return fmt.Errorf("list local followers for remote note event: %w", err)
			}
			for _, relationship := range page {
				recipient, err := h.repo.FindLocalByID(ctx, relationship.FollowerID)
				if err != nil {
					return fmt.Errorf("find local follower for remote note event: %w", err)
				}
				if recipient != nil && recipient.OwnerAccountID != "" {
					recipients[recipient.ID] = recipient
				}
			}
			if len(page) < postDeliveryFollowerLimit {
				break
			}
			next := page[len(page)-1].ID
			if next == "" || next <= afterID {
				return fmt.Errorf("remote note follower pagination did not advance")
			}
			afterID = next
		}
	}
	if note.Visibility == domainnotes.VisibilitySpecified {
		for _, uri := range note.VisibleUserURIs {
			recipient, err := h.repo.FindByURI(ctx, uri)
			if err != nil {
				return fmt.Errorf("find direct recipient for remote note event: %w", err)
			}
			if recipient != nil && recipient.Host == nil && recipient.OwnerAccountID != "" {
				recipients[recipient.ID] = recipient
			}
		}
	}
	for _, recipient := range recipients {
		if err := publish(recipient.OwnerAccountID, recipient.ID); err != nil {
			return err
		}
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

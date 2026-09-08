package worker

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
	apactors "github.com/nexryai/rosmarinus/internal/activitypub/actors"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/idgen"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func (h *Handler) UpdateActor(ctx context.Context, accountID string, command connector.ActorUpdateCommand) (connector.ActorUpdated, error) {
	if h.repo == nil || h.follows == nil || h.queue == nil {
		return connector.ActorUpdated{}, fmt.Errorf("actor repository, follow repository, and queue are required")
	}
	accountID = strings.TrimSpace(accountID)
	actorID := strings.TrimSpace(command.ActorID)
	if accountID == "" || actorID == "" {
		return connector.ActorUpdated{}, fmt.Errorf("owner account id and actor id are required")
	}
	actor, err := h.repo.FindOwnedLocalByID(ctx, accountID, actorID)
	if err != nil {
		return connector.ActorUpdated{}, fmt.Errorf("find owned actor: %w", err)
	}
	if actor == nil {
		return connector.ActorUpdated{}, fmt.Errorf("owned local actor not found: %s", actorID)
	}
	if actor.Host != nil || actor.IsSystemActor {
		return connector.ActorUpdated{}, fmt.Errorf("actor is not an owned local actor: %s", actorID)
	}
	patch := actorPatchFromCommand(command.Patch)
	if patch.IsEmpty() {
		return connector.ActorUpdated{}, fmt.Errorf("at least one actor update field is required")
	}
	if command.Patch.IsPresent("is_locked") && (command.Patch.IsNull("is_locked") || !command.Patch.IsLocked) {
		return connector.ActorUpdated{}, fmt.Errorf("is_locked cannot disable mandatory follow approval")
	}
	if command.Patch.IsPresent("is_bot") && actor.Type != "Person" && actor.Type != "Service" {
		return connector.ActorUpdated{}, fmt.Errorf("is_bot can only update Person or Service actors")
	}
	emojiNames := actor.EmojiNames
	strictEmojiNames := false
	if command.Patch.IsPresent("emoji_names") {
		strictEmojiNames = true
		if command.Patch.IsNull("emoji_names") {
			emojiNames = nil
		} else {
			emojiNames = command.Patch.EmojiNames
		}
	}
	localEmojis, err := h.resolveActorProfileEmojis(ctx, emojiNames, strictEmojiNames)
	if err != nil {
		return connector.ActorUpdated{}, err
	}
	updated, err := h.repo.UpdateOwnedLocalActor(ctx, accountID, actorID, patch)
	if err != nil {
		return connector.ActorUpdated{}, fmt.Errorf("update owned actor: %w", err)
	}
	if updated == nil {
		return connector.ActorUpdated{}, fmt.Errorf("owned local actor disappeared during update: %s", actorID)
	}
	if updated.Host != nil || updated.IsSystemActor {
		return connector.ActorUpdated{}, fmt.Errorf("updated actor is not an owned local actor: %s", actorID)
	}
	published := time.Now().UTC()
	activity := apactors.RenderUpdateAtWithEmojis(h.cfg, updated, localEmojis, "", published)
	if activity == nil {
		return connector.ActorUpdated{}, fmt.Errorf("render actor update: actor is missing")
	}
	if err := h.enqueueActorUpdateDeliveries(ctx, updated, activity); err != nil {
		return connector.ActorUpdated{}, err
	}
	return connector.ActorUpdated{
		ActorID: updated.ID,
		URI:     updated.URI,
		Fields:  actorUpdateChangedFields(command.Patch),
	}, nil
}

func (h *Handler) resolveActorProfileEmojis(ctx context.Context, names []string, strict bool) ([]emojis.Emoji, error) {
	if len(names) == 0 {
		return []emojis.Emoji{}, nil
	}
	if len(names) > 100 {
		return nil, fmt.Errorf("at most 100 actor custom emojis are allowed")
	}
	for _, name := range names {
		if name != strings.TrimSpace(name) || !validLocalEmojiName(name) {
			return nil, fmt.Errorf("invalid actor custom emoji name: %q", name)
		}
	}
	if h.emojis == nil {
		return nil, fmt.Errorf("emoji repository is not configured")
	}
	stored, err := h.emojis.FindLocalByNames(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("resolve actor custom emojis: %w", err)
	}
	if !strict {
		return stored, nil
	}
	byName := make(map[string]emojis.Emoji, len(stored))
	for _, emoji := range stored {
		byName[emoji.Name] = emoji
	}
	for _, name := range names {
		emoji, ok := byName[name]
		if !ok || (emoji.PublicURL == "" && emoji.OriginalURL == "") {
			return nil, fmt.Errorf("local actor custom emoji not found: %s", name)
		}
	}
	return stored, nil
}

func actorPatchFromCommand(data connector.ActorUpdateData) actors.ActorPatch {
	patch := actors.ActorPatch{}
	stringField := func(field, value string) actors.Optional[string] {
		if !data.IsPresent(field) {
			return actors.Optional[string]{}
		}
		if data.IsNull(field) {
			return actors.Clear[string]()
		}
		return actors.Present(value)
	}
	stringSliceField := func(field string, value []string) actors.Optional[[]string] {
		if !data.IsPresent(field) {
			return actors.Optional[[]string]{}
		}
		if data.IsNull(field) {
			return actors.Clear[[]string]()
		}
		return actors.Present(append([]string(nil), value...))
	}
	boolField := func(field string, value bool) actors.Optional[bool] {
		if !data.IsPresent(field) {
			return actors.Optional[bool]{}
		}
		if data.IsNull(field) {
			return actors.Clear[bool]()
		}
		return actors.Present(value)
	}
	patch.Name = stringField("name", data.Name)
	patch.Summary = stringField("summary", data.Summary)
	patch.URL = stringField("url", data.URL)
	patch.Birthday = stringField("birthday", data.Birthday)
	patch.Location = stringField("location", data.Location)
	patch.AvatarURL = stringField("avatar_url", data.AvatarURL)
	patch.BannerURL = stringField("banner_url", data.BannerURL)
	patch.Tags = stringSliceField("tags", data.Tags)
	patch.EmojiNames = stringSliceField("emoji_names", data.EmojiNames)
	patch.IsBot = boolField("is_bot", data.IsBot)
	patch.IsCat = boolField("is_cat", data.IsCat)
	patch.IsLocked = boolField("is_locked", data.IsLocked)
	patch.IsDiscoverable = boolField("is_discoverable", data.IsDiscoverable)
	if data.IsPresent("profile_fields") {
		if data.IsNull("profile_fields") {
			patch.ProfileFields = actors.Clear[[]actors.ProfileField]()
		} else {
			fields := make([]actors.ProfileField, 0, len(data.ProfileFields))
			for _, field := range data.ProfileFields {
				fields = append(fields, actors.ProfileField{Name: field.Name, Value: field.Value})
			}
			patch.ProfileFields = actors.Present(fields)
		}
	}
	return patch
}

func actorUpdateChangedFields(data connector.ActorUpdateData) []string {
	fields := make([]string, 0, 14)
	for _, field := range []string{
		"name", "summary", "url", "profile_fields", "birthday", "location", "avatar_url",
		"banner_url", "tags", "emoji_names", "is_bot", "is_cat", "is_locked", "is_discoverable",
	} {
		if data.IsPresent(field) {
			fields = append(fields, field)
		}
	}
	return fields
}

func (h *Handler) enqueueActorUpdateDeliveries(ctx context.Context, actor *actors.Actor, activity map[string]any) error {
	if actor == nil || h.follows == nil || h.queue == nil {
		return fmt.Errorf("actor, follow repository, and queue are required for actor update delivery")
	}
	destinations := make(map[string]struct{})
	afterID := ""
	for {
		followers, err := h.follows.ListFollowersPage(ctx, actor.ID, afterID, postDeliveryFollowerLimit)
		if err != nil {
			return fmt.Errorf("list followers for actor update delivery: %w", err)
		}
		remoteIDs := make([]string, 0, len(followers))
		for _, follow := range followers {
			if follow.Status == follows.StatusAccepted && follow.FollowerHost != nil {
				remoteIDs = append(remoteIDs, follow.FollowerID)
			}
		}
		activeRemoteIDs, err := h.repo.FilterActiveRemoteIDs(ctx, remoteIDs)
		if err != nil {
			return fmt.Errorf("filter active actor update recipients: %w", err)
		}
		for _, follow := range followers {
			if follow.Status != follows.StatusAccepted || follow.FollowerHost == nil {
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
			if err := h.enqueueActorActivity(ctx, actor.ID, inbox, activity, destinations, isSharedInbox); err != nil {
				return err
			}
		}
		if len(followers) < postDeliveryFollowerLimit {
			break
		}
		afterID = followers[len(followers)-1].ID
	}
	return nil
}

func (h *Handler) enqueueActorActivity(ctx context.Context, actorID, inbox string, activity map[string]any, destinations map[string]struct{}, isSharedInbox bool) error {
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
		return fmt.Errorf("enqueue actor activity delivery to %s: %w", inbox, err)
	}
	return nil
}

func (h *Handler) DeleteActor(ctx context.Context, accountID string, command connector.ActorDeleteCommand) (connector.ActorDeleted, error) {
	if h.repo == nil || h.follows == nil || h.queue == nil {
		return connector.ActorDeleted{}, fmt.Errorf("actor repository, follow repository, and queue are required")
	}
	accountID = strings.TrimSpace(accountID)
	actorID := strings.TrimSpace(command.ActorID)
	if accountID == "" || actorID == "" {
		return connector.ActorDeleted{}, fmt.Errorf("owner account id and actor id are required")
	}
	actor, err := h.repo.FindOwnedLocalByIDIncludingDeleted(ctx, accountID, actorID)
	if err != nil {
		return connector.ActorDeleted{}, fmt.Errorf("find owned actor: %w", err)
	}
	if actor == nil || actor.Host != nil || actor.IsSystemActor {
		return connector.ActorDeleted{}, fmt.Errorf("owned local actor not found: %s", actorID)
	}

	deletedAt := time.Now().UTC()
	if actor.DeletedAt != nil {
		deletedAt = actor.DeletedAt.UTC()
	} else {
		actor, err = h.repo.MarkOwnedLocalActorDeleted(ctx, accountID, actorID, deletedAt)
		if err != nil {
			return connector.ActorDeleted{}, fmt.Errorf("mark owned actor deleted: %w", err)
		}
		if actor == nil || actor.DeletedAt == nil {
			return connector.ActorDeleted{}, fmt.Errorf("owned local actor disappeared during deletion: %s", actorID)
		}
		deletedAt = actor.DeletedAt.UTC()
	}
	activity := apactors.RenderDelete(actor, deletedAt)
	if activity == nil {
		return connector.ActorDeleted{}, fmt.Errorf("render actor delete: actor is missing")
	}
	if err := h.enqueueActorDeleteDeliveries(ctx, actor, activity); err != nil {
		return connector.ActorDeleted{}, err
	}
	if err := h.queue.Enqueue(ctx, queue.NewLocalAccountDeleteTask(actor.ID, actor.URI)); err != nil {
		return connector.ActorDeleted{}, fmt.Errorf("enqueue local account cleanup: %w", err)
	}
	return connector.ActorDeleted{ActorID: actor.ID, URI: actor.URI, DeletedAt: deletedAt}, nil
}

func (h *Handler) ApplyAccountLifecycle(ctx context.Context, accountID string, status account.Status, deleted bool) (int64, error) {
	if h.repo == nil || h.follows == nil || h.queue == nil {
		return 0, fmt.Errorf("actor repository, follow repository, and queue are required")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, fmt.Errorf("owner account id is required")
	}

	var modified int64
	afterID := ""
	for {
		owned, err := h.repo.ListOwnedLocalActorsPage(ctx, accountID, afterID, postDeliveryFollowerLimit, false)
		if err != nil {
			return modified, fmt.Errorf("list owned actors: %w", err)
		}
		for i := range owned {
			actor := &owned[i]
			if deleted || status == account.StatusDeleted {
				if _, err := h.DeleteActor(ctx, accountID, connector.ActorDeleteCommand{ActorID: actor.ID}); err != nil {
					return modified, fmt.Errorf("delete actor %s: %w", actor.ID, err)
				}
				modified++
				continue
			}
			if status == account.StatusActive {
				if !actor.IsSuspended {
					continue
				}
				var suspendedAt *time.Time
				if actor.SuspendedAt != nil {
					value := actor.SuspendedAt.UTC()
					suspendedAt = &value
				}
				resumedAt := time.Now().UTC()
				if suspendedAt != nil {
					if err := h.enqueueActorDeleteDeliveries(ctx, actor, apactors.RenderUnsuspension(actor, *suspendedAt, resumedAt)); err != nil {
						return modified, fmt.Errorf("enqueue actor %s unsuspension: %w", actor.ID, err)
					}
				}
				updated, err := h.repo.SetOwnedLocalActorSuspended(ctx, accountID, actor.ID, false, resumedAt)
				if err != nil {
					return modified, fmt.Errorf("resume actor %s: %w", actor.ID, err)
				}
				if updated == nil {
					continue
				}
				modified++
				continue
			}
			if actor.IsSuspended {
				continue
			}
			suspendedAt := time.Now().UTC()
			if err := h.enqueueActorDeleteDeliveries(ctx, actor, apactors.RenderSuspension(actor, suspendedAt)); err != nil {
				return modified, fmt.Errorf("enqueue actor %s suspension: %w", actor.ID, err)
			}
			updated, err := h.repo.SetOwnedLocalActorSuspended(ctx, accountID, actor.ID, true, suspendedAt)
			if err != nil {
				return modified, fmt.Errorf("suspend actor %s: %w", actor.ID, err)
			}
			if updated == nil || updated.SuspendedAt == nil {
				continue
			}
			modified++
		}
		if len(owned) < postDeliveryFollowerLimit {
			break
		}
		afterID = owned[len(owned)-1].ID
	}
	return modified, nil
}

func (h *Handler) enqueueActorDeleteDeliveries(ctx context.Context, actor *actors.Actor, activity map[string]any) error {
	destinations := make(map[string]struct{})
	if err := h.enqueueActorRelationshipPages(ctx, actor, activity, destinations, true); err != nil {
		return err
	}
	return h.enqueueActorRelationshipPages(ctx, actor, activity, destinations, false)
}

func (h *Handler) enqueueActorRelationshipPages(ctx context.Context, actor *actors.Actor, activity map[string]any, destinations map[string]struct{}, followers bool) error {
	afterID := ""
	for {
		var relationships []follows.Follow
		var err error
		if followers {
			relationships, err = h.follows.ListFollowersPage(ctx, actor.ID, afterID, postDeliveryFollowerLimit)
		} else {
			relationships, err = h.follows.ListFollowingPage(ctx, actor.ID, afterID, postDeliveryFollowerLimit)
		}
		if err != nil {
			return fmt.Errorf("list actor delete relationships: %w", err)
		}
		remoteIDs := make([]string, 0, len(relationships))
		for _, relationship := range relationships {
			if followers && relationship.FollowerHost != nil {
				remoteIDs = append(remoteIDs, relationship.FollowerID)
			} else if !followers && relationship.FolloweeHost != nil {
				remoteIDs = append(remoteIDs, relationship.FolloweeID)
			}
		}
		active, err := h.repo.FilterActiveRemoteIDs(ctx, remoteIDs)
		if err != nil {
			return fmt.Errorf("filter active actor delete recipients: %w", err)
		}
		for _, relationship := range relationships {
			remoteID := relationship.FolloweeID
			host := relationship.FolloweeHost
			inbox := strings.TrimSpace(relationship.FolloweeSharedInbox)
			individualInbox := strings.TrimSpace(relationship.FolloweeInbox)
			if followers {
				remoteID = relationship.FollowerID
				host = relationship.FollowerHost
				inbox = strings.TrimSpace(relationship.FollowerSharedInbox)
				individualInbox = strings.TrimSpace(relationship.FollowerInbox)
			}
			if host == nil || h.cfg.IsFederationHostBlocked(*host) {
				continue
			}
			if _, ok := active[remoteID]; !ok {
				continue
			}
			isSharedInbox := inbox != ""
			if inbox == "" {
				inbox = individualInbox
			}
			if err := h.enqueueActorActivity(ctx, actor.ID, inbox, activity, destinations, isSharedInbox); err != nil {
				return err
			}
		}
		if len(relationships) < postDeliveryFollowerLimit {
			return nil
		}
		nextAfterID := relationships[len(relationships)-1].ID
		if nextAfterID == "" || nextAfterID == afterID {
			return fmt.Errorf("actor delete relationship pagination did not advance")
		}
		afterID = nextAfterID
	}
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
	id, err := idgen.NewUniqueObjectID(ctx, func(ctx context.Context, id string) (bool, error) {
		found, findErr := h.repo.FindAnyByID(ctx, id)
		return found != nil, findErr
	})
	if err != nil {
		return connector.ActorCreated{}, fmt.Errorf("generate local actor id: %w", err)
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
		IsBot:          actorType == "Service",
		IsLocked:       true,
		IsDiscoverable: true,
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
	if actor == nil {
		return connector.ActorCreated{}, fmt.Errorf("created owned actor could not be reloaded")
	}
	return connector.ActorCreated{ActorID: actor.ID, URI: actor.URI, Username: actor.Username}, nil
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

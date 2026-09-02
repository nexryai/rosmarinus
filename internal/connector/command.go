package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

const (
	CommandFollowApprove        = "follow.approve"
	CommandFollowReject         = "follow.reject"
	CommandFollowCreate         = "follow.create"
	CommandFollowDelete         = "follow.delete"
	CommandPostCreate           = "post.create"
	CommandPostDelete           = "post.delete"
	CommandPollVote             = "poll.vote"
	CommandReactionCreate       = "reaction.create"
	CommandReactionDelete       = "reaction.delete"
	CommandBlockCreate          = "block.create"
	CommandBlockDelete          = "block.delete"
	CommandActorCreate          = "actor.create"
	CommandActorUpdate          = "actor.update"
	CommandActorDelete          = "actor.delete"
	CommandNotificationMarkRead = "notification.mark_read"
)

type ActorOwnershipLookup interface {
	FindOwnedLocalByID(context.Context, string, string) (*actors.Actor, error)
	FindOwnedLocalByIDIncludingDeleted(context.Context, string, string) (*actors.Actor, error)
}

type CommandExecutor interface {
	CreateFollow(context.Context, string, string) (string, error)
	DeleteFollow(context.Context, FollowDeleteCommand) (FollowDeleted, error)
	ApproveFollow(context.Context, string, string) (string, error)
	RejectFollow(context.Context, string, string) (string, error)
	CreatePost(context.Context, PostCreateCommand) (PostCreated, error)
	DeletePost(context.Context, PostDeleteCommand) (PostDeleted, error)
	VotePoll(context.Context, PollVoteCommand) (PollVoted, error)
	CreateReaction(context.Context, ReactionCreateCommand) (ReactionCreated, error)
	DeleteReaction(context.Context, ReactionDeleteCommand) (ReactionDeleted, error)
	CreateBlock(context.Context, BlockCreateCommand) (BlockCreated, error)
	DeleteBlock(context.Context, BlockDeleteCommand) (BlockDeleted, error)
	CreateActor(context.Context, string, ActorCreateCommand) (ActorCreated, error)
	UpdateActor(context.Context, string, ActorUpdateCommand) (ActorUpdated, error)
	DeleteActor(context.Context, string, ActorDeleteCommand) (ActorDeleted, error)
	MarkNotificationRead(context.Context, string, string, string) (NotificationRead, error)
}

type FollowApproveData struct {
	FollowerID string `json:"follower_id"`
}

type FollowRejectData struct {
	FollowerID string `json:"follower_id"`
}

type FollowCreateData struct {
	Target string `json:"target"`
}

type FollowDeleteData struct {
	Target string `json:"target"`
}

type FollowDeleteCommand struct {
	ActorID string
	Target  string
}

type FollowDeleted struct {
	FollowerID string `json:"follower_id" bson:"follower_id"`
	FolloweeID string `json:"followee_id" bson:"followee_id"`
	URI        string `json:"uri" bson:"uri"`
}

type PostCreateData struct {
	NoteID         string          `json:"note_id"`
	RenoteID       string          `json:"renote_id,omitempty"`
	Text           string          `json:"text"`
	Visibility     string          `json:"visibility,omitempty"`
	ContentWarning *string         `json:"content_warning,omitempty"`
	Sensitive      bool            `json:"sensitive,omitempty"`
	InReplyToURI   string          `json:"in_reply_to_uri,omitempty"`
	QuoteURI       string          `json:"quote_uri,omitempty"`
	MentionURIs    []string        `json:"mention_uris,omitempty"`
	Hashtags       []string        `json:"hashtags,omitempty"`
	EmojiNames     []string        `json:"emoji_names,omitempty"`
	Poll           *PollCreateData `json:"poll,omitempty"`
}

type PostCreateCommand struct {
	ActorID        string
	NoteID         string
	RenoteID       string
	Text           string
	Visibility     string
	ContentWarning *string
	Sensitive      bool
	InReplyToURI   string
	QuoteURI       string
	MentionURIs    []string
	Hashtags       []string
	EmojiNames     []string
	Poll           *PollCreateCommand
}

type PollCreateData struct {
	Choices   []string   `json:"choices"`
	Multiple  bool       `json:"multiple,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type PollCreateCommand struct {
	Choices   []string
	Multiple  bool
	ExpiresAt *time.Time
}

type PostDeleteData struct {
	NoteID string `json:"note_id"`
}

type PostDeleteCommand struct {
	ActorID string
	NoteID  string
}

type PostDeleted struct {
	ActorID string `json:"actor_id" bson:"actor_id"`
	NoteID  string `json:"note_id" bson:"note_id"`
	URI     string `json:"uri" bson:"uri"`
}

type PollVoteData struct {
	NoteID string `json:"note_id"`
	Choice int    `json:"choice"`
}

type PollVoteCommand struct {
	ActorID string
	NoteID  string
	Choice  int
}

type PollVoted struct {
	VoteID string `json:"vote_id" bson:"vote_id"`
	NoteID string `json:"note_id" bson:"note_id"`
	Choice int    `json:"choice" bson:"choice"`
	URI    string `json:"uri,omitempty" bson:"uri,omitempty"`
}

type ReactionCreateData struct {
	NoteID   string `json:"note_id"`
	Reaction string `json:"reaction"`
}

type ReactionCreateCommand struct {
	ActorID  string
	NoteID   string
	Reaction string
}

type ReactionCreated struct {
	ReactionID string `json:"reaction_id" bson:"reaction_id"`
	NoteID     string `json:"note_id" bson:"note_id"`
	Reaction   string `json:"reaction" bson:"reaction"`
	URI        string `json:"uri" bson:"uri"`
}

type ReactionDeleteData struct {
	NoteID string `json:"note_id"`
}

type ReactionDeleteCommand struct {
	ActorID string
	NoteID  string
}

type ReactionDeleted struct {
	ReactionID string `json:"reaction_id" bson:"reaction_id"`
	NoteID     string `json:"note_id" bson:"note_id"`
	URI        string `json:"uri" bson:"uri"`
}

type BlockCreateData struct {
	Target string `json:"target"`
}

type BlockCreateCommand struct {
	ActorID string
	Target  string
}

type BlockCreated struct {
	BlockID   string `json:"block_id" bson:"block_id"`
	BlockeeID string `json:"blockee_id" bson:"blockee_id"`
	URI       string `json:"uri" bson:"uri"`
}

type BlockDeleteData struct {
	Target string `json:"target"`
}

type BlockDeleteCommand struct {
	ActorID string
	Target  string
}

type BlockDeleted struct {
	BlockID   string `json:"block_id" bson:"block_id"`
	BlockeeID string `json:"blockee_id" bson:"blockee_id"`
	URI       string `json:"uri" bson:"uri"`
}

type ActorCreateData struct {
	Username string `json:"username"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
}

// ActorUpdateData is a patch. Present records fields that were supplied by the
// caller so that a JSON null can clear a value while an omitted field is left
// unchanged. The wire representation remains the plain Salvia-facing object.
type ActorUpdateData struct {
	Name           string                  `json:"name,omitempty"`
	Summary        string                  `json:"summary,omitempty"`
	URL            string                  `json:"url,omitempty"`
	ProfileFields  []ActorProfileFieldData `json:"profile_fields,omitempty"`
	Birthday       string                  `json:"birthday,omitempty"`
	Location       string                  `json:"location,omitempty"`
	AvatarURL      string                  `json:"avatar_url,omitempty"`
	BannerURL      string                  `json:"banner_url,omitempty"`
	Tags           []string                `json:"tags,omitempty"`
	EmojiNames     []string                `json:"emoji_names,omitempty"`
	IsBot          bool                    `json:"is_bot,omitempty"`
	IsCat          bool                    `json:"is_cat,omitempty"`
	IsLocked       bool                    `json:"is_locked,omitempty"`
	IsDiscoverable bool                    `json:"is_discoverable,omitempty"`
	Present        map[string]bool         `json:"-"`
	Null           map[string]bool         `json:"-"`
}

type ActorProfileFieldData struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ActorUpdateCommand struct {
	ActorID string
	Patch   ActorUpdateData
}

type ActorUpdated struct {
	ActorID string   `json:"actor_id" bson:"actor_id"`
	URI     string   `json:"uri" bson:"uri"`
	Fields  []string `json:"fields,omitempty" bson:"fields,omitempty"`
}

type ActorDeleteCommand struct {
	ActorID string
}

type ActorDeleted struct {
	ActorID   string    `json:"actor_id" bson:"actor_id"`
	URI       string    `json:"uri" bson:"uri"`
	DeletedAt time.Time `json:"deleted_at" bson:"deleted_at"`
}

type NotificationMarkReadData struct {
	NotificationID string `json:"notification_id"`
}

type NotificationRead struct {
	NotificationID string `json:"notification_id" bson:"notification_id"`
	IsRead         bool   `json:"is_read" bson:"is_read"`
}

type ActorCreateCommand struct {
	Username string
	Name     string
	Type     string
}

type ActorCreated struct {
	ActorID  string `json:"actor_id" bson:"actor_id"`
	URI      string `json:"uri" bson:"uri"`
	Username string `json:"username" bson:"username"`
}

var actorUpdateFields = []string{
	"name", "summary", "url", "profile_fields", "birthday", "location",
	"avatar_url", "banner_url", "tags", "emoji_names", "is_bot", "is_cat",
	"is_locked", "is_discoverable",
}

var actorUpdateImmutableFields = map[string]struct{}{
	"id": {}, "actor_id": {}, "username": {}, "username_lower": {}, "type": {},
	"owner_account_id": {}, "uri": {}, "inbox": {}, "shared_inbox": {},
	"followers_uri": {}, "following_uri": {}, "featured_uri": {},
	"public_key_id": {}, "public_key_pem": {}, "private_key_pem": {},
}

var actorUpdateMutableFields = func() map[string]struct{} {
	fields := make(map[string]struct{}, len(actorUpdateFields))
	for _, field := range actorUpdateFields {
		fields[field] = struct{}{}
	}
	return fields
}()

func (d ActorUpdateData) HasChanges() bool {
	for _, field := range actorUpdateFields {
		if d.fieldPresent(field) {
			return true
		}
	}
	return false
}

func (d ActorUpdateData) fieldPresent(field string) bool {
	if d.Present != nil {
		return d.Present[field]
	}
	switch field {
	case "name":
		return d.Name != ""
	case "summary":
		return d.Summary != ""
	case "url":
		return d.URL != ""
	case "profile_fields":
		return len(d.ProfileFields) > 0
	case "birthday":
		return d.Birthday != ""
	case "location":
		return d.Location != ""
	case "avatar_url":
		return d.AvatarURL != ""
	case "banner_url":
		return d.BannerURL != ""
	case "tags":
		return len(d.Tags) > 0
	case "emoji_names":
		return len(d.EmojiNames) > 0
	case "is_bot":
		return d.IsBot
	case "is_cat":
		return d.IsCat
	case "is_locked":
		return d.IsLocked
	case "is_discoverable":
		return d.IsDiscoverable
	default:
		return false
	}
}

func (d ActorUpdateData) IsPresent(field string) bool {
	return d.fieldPresent(field)
}

func (d ActorUpdateData) IsNull(field string) bool {
	return d.Present != nil && d.Present[field] && d.Null != nil && d.Null[field]
}

func (d ActorUpdateData) MarshalJSON() ([]byte, error) {
	values := make(map[string]any)
	value := func(field string, value any) any {
		if d.IsNull(field) {
			return nil
		}
		return value
	}
	if d.fieldPresent("name") {
		values["name"] = value("name", d.Name)
	}
	if d.fieldPresent("summary") {
		values["summary"] = value("summary", d.Summary)
	}
	if d.fieldPresent("url") {
		values["url"] = value("url", d.URL)
	}
	if d.fieldPresent("profile_fields") {
		values["profile_fields"] = value("profile_fields", d.ProfileFields)
	}
	if d.fieldPresent("birthday") {
		values["birthday"] = value("birthday", d.Birthday)
	}
	if d.fieldPresent("location") {
		values["location"] = value("location", d.Location)
	}
	if d.fieldPresent("avatar_url") {
		values["avatar_url"] = value("avatar_url", d.AvatarURL)
	}
	if d.fieldPresent("banner_url") {
		values["banner_url"] = value("banner_url", d.BannerURL)
	}
	if d.fieldPresent("tags") {
		values["tags"] = value("tags", d.Tags)
	}
	if d.fieldPresent("emoji_names") {
		values["emoji_names"] = value("emoji_names", d.EmojiNames)
	}
	if d.fieldPresent("is_bot") {
		values["is_bot"] = value("is_bot", d.IsBot)
	}
	if d.fieldPresent("is_cat") {
		values["is_cat"] = value("is_cat", d.IsCat)
	}
	if d.fieldPresent("is_locked") {
		values["is_locked"] = value("is_locked", d.IsLocked)
	}
	if d.fieldPresent("is_discoverable") {
		values["is_discoverable"] = value("is_discoverable", d.IsDiscoverable)
	}
	return json.Marshal(values)
}

func (d *ActorUpdateData) UnmarshalJSON(raw []byte) error {
	var decoded struct {
		Name           string                  `json:"name"`
		Summary        string                  `json:"summary"`
		URL            string                  `json:"url"`
		ProfileFields  []ActorProfileFieldData `json:"profile_fields"`
		Birthday       string                  `json:"birthday"`
		Location       string                  `json:"location"`
		AvatarURL      string                  `json:"avatar_url"`
		BannerURL      string                  `json:"banner_url"`
		Tags           []string                `json:"tags"`
		EmojiNames     []string                `json:"emoji_names"`
		IsBot          bool                    `json:"is_bot"`
		IsCat          bool                    `json:"is_cat"`
		IsLocked       bool                    `json:"is_locked"`
		IsDiscoverable bool                    `json:"is_discoverable"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode actor update patch: %w", err)
	}
	var supplied map[string]json.RawMessage
	if err := json.Unmarshal(raw, &supplied); err != nil {
		return fmt.Errorf("decode actor update fields: %w", err)
	}
	for field := range supplied {
		if _, immutable := actorUpdateImmutableFields[field]; immutable {
			return fmt.Errorf("actor update field %q is immutable", field)
		}
		if _, mutable := actorUpdateMutableFields[field]; !mutable {
			return fmt.Errorf("unsupported actor update field %q", field)
		}
	}
	*d = ActorUpdateData{
		Name:           decoded.Name,
		Summary:        decoded.Summary,
		URL:            decoded.URL,
		ProfileFields:  decoded.ProfileFields,
		Birthday:       decoded.Birthday,
		Location:       decoded.Location,
		AvatarURL:      decoded.AvatarURL,
		BannerURL:      decoded.BannerURL,
		Tags:           decoded.Tags,
		EmojiNames:     decoded.EmojiNames,
		IsBot:          decoded.IsBot,
		IsCat:          decoded.IsCat,
		IsLocked:       decoded.IsLocked,
		IsDiscoverable: decoded.IsDiscoverable,
		Present:        make(map[string]bool, len(supplied)),
		Null:           make(map[string]bool),
	}
	for field := range supplied {
		d.Present[field] = true
		d.Null[field] = string(supplied[field]) == "null"
	}
	return nil
}

// ExecuteCommand validates and executes a user-facing operation independently
// of HTTP. Account and Actor authorization must be completed before calling it.
func ExecuteCommand(ctx context.Context, executor CommandExecutor, name, accountID, actorID string, data any) (any, string, error) {
	if executor == nil {
		return nil, actorID, fmt.Errorf("command executor is not configured")
	}
	switch name {
	case CommandFollowCreate:
		var command FollowCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.Target) == "" {
			return nil, actorID, fmt.Errorf("target is required")
		}
		result, err := executor.CreateFollow(ctx, actorID, command.Target)
		return result, actorID, err
	case CommandFollowDelete:
		var command FollowDeleteData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.Target) == "" {
			return nil, actorID, fmt.Errorf("target is required")
		}
		result, err := executor.DeleteFollow(ctx, FollowDeleteCommand{
			ActorID: actorID,
			Target:  command.Target,
		})
		return result, actorID, err
	case CommandFollowApprove:
		var command FollowApproveData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.FollowerID) == "" {
			return nil, actorID, fmt.Errorf("follower_id is required")
		}
		result, err := executor.ApproveFollow(ctx, command.FollowerID, actorID)
		return result, actorID, err
	case CommandFollowReject:
		var command FollowRejectData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.FollowerID) == "" {
			return nil, actorID, fmt.Errorf("follower_id is required")
		}
		result, err := executor.RejectFollow(ctx, command.FollowerID, actorID)
		return result, actorID, err
	case CommandPostCreate:
		var command PostCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		hasContent := strings.TrimSpace(command.Text) != "" || command.Poll != nil
		hasRenote := strings.TrimSpace(command.RenoteID) != ""
		if strings.TrimSpace(command.NoteID) == "" || hasContent == hasRenote {
			return nil, actorID, fmt.Errorf("note_id and exactly one of text or poll, or renote_id are required")
		}
		var poll *PollCreateCommand
		if command.Poll != nil {
			poll = &PollCreateCommand{Choices: command.Poll.Choices, Multiple: command.Poll.Multiple, ExpiresAt: command.Poll.ExpiresAt}
		}
		result, err := executor.CreatePost(ctx, PostCreateCommand{
			ActorID:        actorID,
			NoteID:         command.NoteID,
			RenoteID:       command.RenoteID,
			Text:           command.Text,
			Visibility:     command.Visibility,
			ContentWarning: command.ContentWarning,
			Sensitive:      command.Sensitive,
			InReplyToURI:   command.InReplyToURI,
			QuoteURI:       command.QuoteURI,
			MentionURIs:    command.MentionURIs,
			Hashtags:       command.Hashtags,
			EmojiNames:     command.EmojiNames,
			Poll:           poll,
		})
		return result, actorID, err
	case CommandPostDelete:
		var command PostDeleteData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NoteID) == "" {
			return nil, actorID, fmt.Errorf("note_id is required")
		}
		result, err := executor.DeletePost(ctx, PostDeleteCommand{ActorID: actorID, NoteID: command.NoteID})
		return result, actorID, err
	case CommandPollVote:
		var command PollVoteData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NoteID) == "" || command.Choice < 0 {
			return nil, actorID, fmt.Errorf("note_id and a non-negative choice are required")
		}
		result, err := executor.VotePoll(ctx, PollVoteCommand{ActorID: actorID, NoteID: command.NoteID, Choice: command.Choice})
		return result, actorID, err
	case CommandReactionCreate:
		var command ReactionCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NoteID) == "" || strings.TrimSpace(command.Reaction) == "" {
			return nil, actorID, fmt.Errorf("note_id and reaction are required")
		}
		result, err := executor.CreateReaction(ctx, ReactionCreateCommand{
			ActorID:  actorID,
			NoteID:   command.NoteID,
			Reaction: command.Reaction,
		})
		return result, actorID, err
	case CommandReactionDelete:
		var command ReactionDeleteData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NoteID) == "" {
			return nil, actorID, fmt.Errorf("note_id is required")
		}
		result, err := executor.DeleteReaction(ctx, ReactionDeleteCommand{
			ActorID: actorID,
			NoteID:  command.NoteID,
		})
		return result, actorID, err
	case CommandBlockCreate:
		var command BlockCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.Target) == "" {
			return nil, actorID, fmt.Errorf("target is required")
		}
		result, err := executor.CreateBlock(ctx, BlockCreateCommand{ActorID: actorID, Target: command.Target})
		return result, actorID, err
	case CommandBlockDelete:
		var command BlockDeleteData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.Target) == "" {
			return nil, actorID, fmt.Errorf("target is required")
		}
		result, err := executor.DeleteBlock(ctx, BlockDeleteCommand{ActorID: actorID, Target: command.Target})
		return result, actorID, err
	case CommandNotificationMarkRead:
		var command NotificationMarkReadData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NotificationID) == "" {
			return nil, actorID, fmt.Errorf("notification_id is required")
		}
		result, err := executor.MarkNotificationRead(ctx, accountID, actorID, command.NotificationID)
		return result, actorID, err
	case CommandActorCreate:
		var command ActorCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(command.Username) == "" {
			return nil, "", fmt.Errorf("username is required")
		}
		result, err := executor.CreateActor(ctx, accountID, ActorCreateCommand{
			Username: command.Username,
			Name:     command.Name,
			Type:     command.Type,
		})
		return result, result.ActorID, err
	case CommandActorUpdate:
		var command ActorUpdateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if !command.HasChanges() {
			return nil, actorID, fmt.Errorf("at least one actor update field is required")
		}
		if command.IsPresent("is_locked") && (command.IsNull("is_locked") || !command.IsLocked) {
			return nil, actorID, fmt.Errorf("is_locked cannot disable mandatory follow approval")
		}
		result, err := executor.UpdateActor(ctx, accountID, ActorUpdateCommand{ActorID: actorID, Patch: command})
		return result, actorID, err
	case CommandActorDelete:
		if err := requireEmptyCommandData(data); err != nil {
			return nil, actorID, err
		}
		result, err := executor.DeleteActor(ctx, accountID, ActorDeleteCommand{ActorID: actorID})
		return result, actorID, err
	default:
		return nil, actorID, fmt.Errorf("unknown operation: %s", name)
	}
}

func requireEmptyCommandData(data any) error {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode command data: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("command data must be an object: %w", err)
	}
	if len(fields) != 0 {
		return fmt.Errorf("actor.delete data must be empty")
	}
	return nil
}

func decodeCommandData(data any, out any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal command data: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode command data: %w", err)
	}
	return nil
}

package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

const (
	CommandFollowApprove        = "follow.approve"
	CommandFollowReject         = "follow.reject"
	CommandFollowCreate         = "follow.create"
	CommandFollowDelete         = "follow.delete"
	CommandPostCreate           = "post.create"
	CommandReactionCreate       = "reaction.create"
	CommandReactionDelete       = "reaction.delete"
	CommandActorCreate          = "actor.create"
	CommandNotificationMarkRead = "notification.mark_read"
)

type CommandMessage struct {
	ID       string
	ClientID string
	Name     string
	Data     any
}

type CommandEnvelope struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	ActorID   string `json:"actor_id"`
	Data      any    `json:"data"`
}

type CommandSource interface {
	Subscribe(context.Context, string, func(CommandMessage)) (func(), error)
}

type AccountLookup interface {
	FindActiveByAblyClientID(context.Context, string) (*account.Account, error)
}

type ActorOwnershipLookup interface {
	FindOwnedLocalByID(context.Context, string, string) (*actors.Actor, error)
}

type CommandExecutor interface {
	CreateFollow(context.Context, string, string) (string, error)
	DeleteFollow(context.Context, FollowDeleteCommand) (FollowDeleted, error)
	ApproveFollow(context.Context, string, string) (string, error)
	RejectFollow(context.Context, string, string) (string, error)
	CreatePost(context.Context, PostCreateCommand) (PostCreated, error)
	CreateReaction(context.Context, ReactionCreateCommand) (ReactionCreated, error)
	DeleteReaction(context.Context, ReactionDeleteCommand) (ReactionDeleted, error)
	CreateActor(context.Context, string, ActorCreateCommand) (ActorCreated, error)
	MarkNotificationRead(context.Context, string, string, string) (NotificationRead, error)
}

type CommandResultPublisher interface {
	PublishCommandSucceeded(context.Context, string, string, string, string, any) error
	PublishCommandFailed(context.Context, string, string, string, string, string) error
	PublishActorCreated(context.Context, string, string, ActorCreated) error
}

type CommandHandler struct {
	source     CommandSource
	accounts   AccountLookup
	actors     ActorOwnershipLookup
	executor   CommandExecutor
	publisher  CommandResultPublisher
	receipts   CommandReceiptStore
	logger     *log.Logger
	now        func() time.Time
	receiptTTL time.Duration
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
	NoteID         string   `json:"note_id"`
	Text           string   `json:"text"`
	Visibility     string   `json:"visibility,omitempty"`
	ContentWarning *string  `json:"content_warning,omitempty"`
	Sensitive      bool     `json:"sensitive,omitempty"`
	InReplyToURI   string   `json:"in_reply_to_uri,omitempty"`
	QuoteURI       string   `json:"quote_uri,omitempty"`
	MentionURIs    []string `json:"mention_uris,omitempty"`
	Hashtags       []string `json:"hashtags,omitempty"`
}

type PostCreateCommand struct {
	ActorID        string
	NoteID         string
	Text           string
	Visibility     string
	ContentWarning *string
	Sensitive      bool
	InReplyToURI   string
	QuoteURI       string
	MentionURIs    []string
	Hashtags       []string
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

type ActorCreateData struct {
	Username string `json:"username"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
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

func NewCommandHandler(source CommandSource, accounts AccountLookup, actorLookup ActorOwnershipLookup, executor CommandExecutor, publisher CommandResultPublisher, receipts CommandReceiptStore, logger *log.Logger, receiptTTL time.Duration) *CommandHandler {
	if receiptTTL <= 0 {
		receiptTTL = 7 * 24 * time.Hour
	}
	return &CommandHandler{
		source:     source,
		accounts:   accounts,
		actors:     actorLookup,
		executor:   executor,
		publisher:  publisher,
		receipts:   receipts,
		logger:     logger,
		now:        func() time.Time { return time.Now().UTC() },
		receiptTTL: receiptTTL,
	}
}

func (h *CommandHandler) Subscribe(ctx context.Context) (func(), error) {
	if h == nil || h.source == nil {
		return func() {}, nil
	}
	names := []string{CommandFollowCreate, CommandFollowDelete, CommandFollowApprove, CommandFollowReject, CommandPostCreate, CommandReactionCreate, CommandReactionDelete, CommandActorCreate, CommandNotificationMarkRead}
	unsubscribes := make([]func(), 0, len(names))
	for _, name := range names {
		unsubscribe, err := h.source.Subscribe(ctx, name, func(message CommandMessage) {
			if err := h.Handle(ctx, message); err != nil && h.logger != nil {
				h.logger.Printf("connector: command failed name=%s message_id=%s client_id=%s err=%v", message.Name, message.ID, message.ClientID, err)
			}
		})
		if err != nil {
			for i := len(unsubscribes) - 1; i >= 0; i-- {
				unsubscribes[i]()
			}
			return nil, err
		}
		unsubscribes = append(unsubscribes, unsubscribe)
	}
	return func() {
		for i := len(unsubscribes) - 1; i >= 0; i-- {
			unsubscribes[i]()
		}
	}, nil
}

func (h *CommandHandler) Handle(ctx context.Context, message CommandMessage) error {
	if h == nil {
		return fmt.Errorf("connector command handler is not configured")
	}
	name := strings.TrimSpace(message.Name)
	if !supportedCommand(name) {
		if name == "" {
			return fmt.Errorf("connector command name is required")
		}
		return fmt.Errorf("unknown connector command: %s", name)
	}
	envelope, err := decodeEnvelope(message.Data)
	if err != nil {
		return err
	}
	accountRecord, err := h.authorizeAccount(ctx, message.ClientID)
	if err != nil {
		return err
	}
	actorID := envelope.ActorID
	if name != CommandActorCreate {
		actor, err := h.authorizeActor(ctx, accountRecord.ID, envelope.ActorID)
		if err != nil {
			return err
		}
		actorID = actor.ID
	}

	receipt := CommandReceipt{
		AccountID: accountRecord.ID,
		ClientID:  message.ClientID,
		RequestID: envelope.RequestID,
		Command:   name,
		ActorID:   actorID,
		Status:    CommandReceiptPending,
		CreatedAt: h.now(),
		UpdatedAt: h.now(),
		ExpiresAt: h.now().Add(h.receiptTTL),
	}
	if h.receipts != nil {
		existing, claimed, err := h.receipts.Claim(ctx, receipt)
		if err != nil {
			return fmt.Errorf("claim connector command receipt: %w", err)
		}
		if !claimed {
			return h.republishReceipt(ctx, existing)
		}
	}

	result, resultActorID, err := h.execute(ctx, name, accountRecord.ID, actorID, envelope.Data)
	if err != nil {
		code := "command_failed"
		if h.logger != nil {
			h.logger.Printf("connector: command execution failed account_id=%s client_id=%s actor_id=%s name=%s request_id=%s message_id=%s err=%v", accountRecord.ID, message.ClientID, actorID, name, envelope.RequestID, message.ID, err)
		}
		if h.receipts != nil {
			if receiptErr := h.receipts.Fail(ctx, accountRecord.ID, envelope.RequestID, code, h.now()); receiptErr != nil {
				return fmt.Errorf("record connector command failure after %v: %w", err, receiptErr)
			}
		}
		if publishErr := h.publishFailed(ctx, accountRecord.ID, envelope.RequestID, actorID, name, code); publishErr != nil {
			return fmt.Errorf("publish connector command failure after %v: %w", err, publishErr)
		}
		return err
	}
	if h.receipts != nil {
		if err := h.receipts.Complete(ctx, accountRecord.ID, envelope.RequestID, resultActorID, result, h.now()); err != nil {
			return fmt.Errorf("complete connector command receipt: %w", err)
		}
	}
	if h.publisher != nil {
		if name == CommandActorCreate {
			created, ok := result.(ActorCreated)
			if !ok {
				return fmt.Errorf("actor.create returned unexpected result type %T", result)
			}
			if err := h.publisher.PublishActorCreated(ctx, accountRecord.ID, envelope.RequestID, created); err != nil {
				return fmt.Errorf("publish actor.created event: %w", err)
			}
		}
		if err := h.publisher.PublishCommandSucceeded(ctx, accountRecord.ID, envelope.RequestID, resultActorID, name, result); err != nil {
			return fmt.Errorf("publish connector command result: %w", err)
		}
	}
	return nil
}

func (h *CommandHandler) authorizeAccount(ctx context.Context, clientID string) (*account.Account, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, fmt.Errorf("connector command clientId is required")
	}
	if h.accounts == nil {
		return nil, fmt.Errorf("Salvia account lookup is not configured")
	}
	accountRecord, err := h.accounts.FindActiveByAblyClientID(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("resolve connector account: %w", err)
	}
	if !accountRecord.IsActive() {
		return nil, fmt.Errorf("connector account is not active")
	}
	return accountRecord, nil
}

func (h *CommandHandler) authorizeActor(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("connector command actor_id is required")
	}
	if h.actors == nil {
		return nil, fmt.Errorf("actor ownership lookup is not configured")
	}
	actor, err := h.actors.FindOwnedLocalByID(ctx, accountID, actorID)
	if err != nil {
		return nil, fmt.Errorf("authorize connector actor: %w", err)
	}
	if actor == nil {
		return nil, fmt.Errorf("connector actor is not owned by account")
	}
	return actor, nil
}

func (h *CommandHandler) execute(ctx context.Context, name, accountID, actorID string, data any) (any, string, error) {
	if h.executor == nil {
		return nil, actorID, fmt.Errorf("connector command executor is not configured")
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
		result, err := h.executor.CreateFollow(ctx, actorID, command.Target)
		return result, actorID, err
	case CommandFollowDelete:
		var command FollowDeleteData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.Target) == "" {
			return nil, actorID, fmt.Errorf("target is required")
		}
		result, err := h.executor.DeleteFollow(ctx, FollowDeleteCommand{
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
		result, err := h.executor.ApproveFollow(ctx, command.FollowerID, actorID)
		return result, actorID, err
	case CommandFollowReject:
		var command FollowRejectData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.FollowerID) == "" {
			return nil, actorID, fmt.Errorf("follower_id is required")
		}
		result, err := h.executor.RejectFollow(ctx, command.FollowerID, actorID)
		return result, actorID, err
	case CommandPostCreate:
		var command PostCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NoteID) == "" || strings.TrimSpace(command.Text) == "" {
			return nil, actorID, fmt.Errorf("note_id and text are required")
		}
		result, err := h.executor.CreatePost(ctx, PostCreateCommand{
			ActorID:        actorID,
			NoteID:         command.NoteID,
			Text:           command.Text,
			Visibility:     command.Visibility,
			ContentWarning: command.ContentWarning,
			Sensitive:      command.Sensitive,
			InReplyToURI:   command.InReplyToURI,
			QuoteURI:       command.QuoteURI,
			MentionURIs:    command.MentionURIs,
			Hashtags:       command.Hashtags,
		})
		return result, actorID, err
	case CommandReactionCreate:
		var command ReactionCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NoteID) == "" || strings.TrimSpace(command.Reaction) == "" {
			return nil, actorID, fmt.Errorf("note_id and reaction are required")
		}
		result, err := h.executor.CreateReaction(ctx, ReactionCreateCommand{
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
		result, err := h.executor.DeleteReaction(ctx, ReactionDeleteCommand{
			ActorID: actorID,
			NoteID:  command.NoteID,
		})
		return result, actorID, err
	case CommandNotificationMarkRead:
		var command NotificationMarkReadData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, actorID, err
		}
		if strings.TrimSpace(command.NotificationID) == "" {
			return nil, actorID, fmt.Errorf("notification_id is required")
		}
		result, err := h.executor.MarkNotificationRead(ctx, accountID, actorID, command.NotificationID)
		return result, actorID, err
	case CommandActorCreate:
		var command ActorCreateData
		if err := decodeCommandData(data, &command); err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(command.Username) == "" {
			return nil, "", fmt.Errorf("username is required")
		}
		result, err := h.executor.CreateActor(ctx, accountID, ActorCreateCommand{
			Username: command.Username,
			Name:     command.Name,
			Type:     command.Type,
		})
		return result, result.ActorID, err
	default:
		return nil, actorID, fmt.Errorf("unknown connector command: %s", name)
	}
}

func (h *CommandHandler) republishReceipt(ctx context.Context, receipt *CommandReceipt) error {
	if receipt == nil {
		return fmt.Errorf("connector command receipt conflict without existing receipt")
	}
	switch receipt.Status {
	case CommandReceiptCompleted:
		if h.publisher == nil {
			return nil
		}
		return h.publisher.PublishCommandSucceeded(ctx, receipt.AccountID, receipt.RequestID, receipt.ActorID, receipt.Command, receipt.Result)
	case CommandReceiptFailed:
		return h.publishFailed(ctx, receipt.AccountID, receipt.RequestID, receipt.ActorID, receipt.Command, receipt.ErrorCode)
	case CommandReceiptPending:
		return h.publishFailed(ctx, receipt.AccountID, receipt.RequestID, receipt.ActorID, receipt.Command, "command_in_progress")
	default:
		return fmt.Errorf("unsupported connector command receipt status: %s", receipt.Status)
	}
}

func (h *CommandHandler) publishFailed(ctx context.Context, accountID, requestID, actorID, command, code string) error {
	if h.publisher == nil {
		return nil
	}
	return h.publisher.PublishCommandFailed(ctx, accountID, requestID, actorID, command, code)
}

func supportedCommand(name string) bool {
	switch name {
	case CommandFollowCreate, CommandFollowDelete, CommandFollowApprove, CommandFollowReject, CommandPostCreate, CommandReactionCreate, CommandReactionDelete, CommandActorCreate, CommandNotificationMarkRead:
		return true
	default:
		return false
	}
}

func decodeEnvelope(data any) (CommandEnvelope, error) {
	var envelope CommandEnvelope
	if err := decodeCommandData(data, &envelope); err != nil {
		return CommandEnvelope{}, err
	}
	if envelope.Version != 1 {
		return CommandEnvelope{}, fmt.Errorf("unsupported connector command version: %d", envelope.Version)
	}
	envelope.RequestID = strings.TrimSpace(envelope.RequestID)
	envelope.ActorID = strings.TrimSpace(envelope.ActorID)
	if envelope.RequestID == "" {
		return CommandEnvelope{}, fmt.Errorf("connector command request_id is required")
	}
	return envelope, nil
}

func decodeCommandData(data any, out any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal connector command data: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode connector command data: %w", err)
	}
	return nil
}

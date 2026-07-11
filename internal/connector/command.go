package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	CommandFollowApprove        = "follow.approve"
	CommandFollowReject         = "follow.reject"
	CommandPostCreate           = "post.create"
	CommandNotificationMarkRead = "notification.mark_read"
)

type CommandMessage struct {
	Name string
	Data any
}

type CommandSource interface {
	Subscribe(context.Context, string, func(CommandMessage)) (func(), error)
}

type FollowApprover interface {
	ApproveFollow(context.Context, string, string) (string, error)
	RejectFollow(context.Context, string, string) (string, error)
	CreatePost(context.Context, PostCreateCommand) (PostCreated, error)
}

type CommandHandler struct {
	source         CommandSource
	followApprover FollowApprover
}

type FollowApproveCommand struct {
	FollowerID string `json:"follower_id"`
	FolloweeID string `json:"followee_id"`
}

type FollowRejectCommand struct {
	FollowerID string `json:"follower_id"`
	FolloweeID string `json:"followee_id"`
}

type PostCreateCommand struct {
	ActorID        string   `json:"actor_id"`
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

func NewCommandHandler(source CommandSource, followApprover FollowApprover) *CommandHandler {
	return &CommandHandler{source: source, followApprover: followApprover}
}

func (h *CommandHandler) Subscribe(ctx context.Context) (func(), error) {
	if h == nil || h.source == nil {
		return func() {}, nil
	}
	unsubscribeApprove, err := h.source.Subscribe(ctx, CommandFollowApprove, func(message CommandMessage) {
		_ = h.Handle(ctx, message)
	})
	if err != nil {
		return nil, err
	}
	unsubscribeReject, err := h.source.Subscribe(ctx, CommandFollowReject, func(message CommandMessage) {
		_ = h.Handle(ctx, message)
	})
	if err != nil {
		unsubscribeApprove()
		return nil, err
	}
	unsubscribePostCreate, err := h.source.Subscribe(ctx, CommandPostCreate, func(message CommandMessage) {
		_ = h.Handle(ctx, message)
	})
	if err != nil {
		unsubscribeReject()
		unsubscribeApprove()
		return nil, err
	}
	return func() {
		unsubscribePostCreate()
		unsubscribeReject()
		unsubscribeApprove()
	}, nil
}

func (h *CommandHandler) Handle(ctx context.Context, message CommandMessage) error {
	name := strings.TrimSpace(message.Name)
	switch name {
	case CommandFollowApprove:
		return h.handleFollowApprove(ctx, message.Data)
	case CommandFollowReject:
		return h.handleFollowReject(ctx, message.Data)
	case CommandPostCreate:
		return h.handlePostCreate(ctx, message.Data)
	case CommandNotificationMarkRead:
		return fmt.Errorf("connector command %s is not implemented", name)
	case "":
		return fmt.Errorf("connector command name is required")
	default:
		return fmt.Errorf("unknown connector command: %s", name)
	}
}

func (h *CommandHandler) handlePostCreate(ctx context.Context, data any) error {
	if h.followApprover == nil {
		return fmt.Errorf("post creator is not configured")
	}
	var command PostCreateCommand
	if err := decodeCommandData(data, &command); err != nil {
		return err
	}
	if strings.TrimSpace(command.ActorID) == "" || strings.TrimSpace(command.NoteID) == "" {
		return fmt.Errorf("actor_id and note_id are required")
	}
	if strings.TrimSpace(command.Text) == "" {
		return fmt.Errorf("text is required")
	}
	_, err := h.followApprover.CreatePost(ctx, command)
	return err
}

func (h *CommandHandler) handleFollowReject(ctx context.Context, data any) error {
	if h.followApprover == nil {
		return fmt.Errorf("follow approver is not configured")
	}
	var command FollowRejectCommand
	if err := decodeCommandData(data, &command); err != nil {
		return err
	}
	if strings.TrimSpace(command.FollowerID) == "" || strings.TrimSpace(command.FolloweeID) == "" {
		return fmt.Errorf("follower_id and followee_id are required")
	}
	_, err := h.followApprover.RejectFollow(ctx, command.FollowerID, command.FolloweeID)
	return err
}

func (h *CommandHandler) handleFollowApprove(ctx context.Context, data any) error {
	if h.followApprover == nil {
		return fmt.Errorf("follow approver is not configured")
	}
	var command FollowApproveCommand
	if err := decodeCommandData(data, &command); err != nil {
		return err
	}
	if strings.TrimSpace(command.FollowerID) == "" || strings.TrimSpace(command.FolloweeID) == "" {
		return fmt.Errorf("follower_id and followee_id are required")
	}
	_, err := h.followApprover.ApproveFollow(ctx, command.FollowerID, command.FolloweeID)
	return err
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

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
}

type CommandHandler struct {
	source         CommandSource
	followApprover FollowApprover
}

type FollowApproveCommand struct {
	FollowerID string `json:"follower_id"`
	FolloweeID string `json:"followee_id"`
}

func NewCommandHandler(source CommandSource, followApprover FollowApprover) *CommandHandler {
	return &CommandHandler{source: source, followApprover: followApprover}
}

func (h *CommandHandler) Subscribe(ctx context.Context) (func(), error) {
	if h == nil || h.source == nil {
		return func() {}, nil
	}
	return h.source.Subscribe(ctx, CommandFollowApprove, func(message CommandMessage) {
		_ = h.Handle(ctx, message)
	})
}

func (h *CommandHandler) Handle(ctx context.Context, message CommandMessage) error {
	name := strings.TrimSpace(message.Name)
	switch name {
	case CommandFollowApprove:
		return h.handleFollowApprove(ctx, message.Data)
	case CommandFollowReject, CommandPostCreate, CommandNotificationMarkRead:
		return fmt.Errorf("connector command %s is not implemented", name)
	case "":
		return fmt.Errorf("connector command name is required")
	default:
		return fmt.Errorf("unknown connector command: %s", name)
	}
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

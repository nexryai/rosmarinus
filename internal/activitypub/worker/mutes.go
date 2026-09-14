package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/mutes"
)

func (h *Handler) CreateMute(ctx context.Context, command connector.MuteCreateCommand) (connector.MuteCreated, error) {
	if h.mutes == nil {
		return connector.MuteCreated{}, fmt.Errorf("mute repository is not configured")
	}
	muter, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.MuteCreated{}, err
	}
	if muter == nil {
		return connector.MuteCreated{}, fmt.Errorf("local muter not found: %s", command.ActorID)
	}
	mutee, err := h.resolveMuteTarget(ctx, command.Target)
	if err != nil {
		return connector.MuteCreated{}, fmt.Errorf("resolve mute target: %w", err)
	}
	if muter.ID == mutee.ID {
		return connector.MuteCreated{}, fmt.Errorf("an actor cannot mute itself")
	}
	now := time.Now().UTC()
	if command.ExpiresAt != nil && !command.ExpiresAt.After(now) {
		return connector.MuteCreated{}, fmt.Errorf("mute expiration must be in the future")
	}
	stored, err := h.mutes.Upsert(ctx, mutes.Mute{MuterID: muter.ID, MuteeID: mutee.ID, CreatedAt: now, ExpiresAt: command.ExpiresAt})
	if err != nil {
		return connector.MuteCreated{}, err
	}
	return connector.MuteCreated{MuteID: stored.ID, MuteeID: stored.MuteeID, ExpiresAt: stored.ExpiresAt}, nil
}

func (h *Handler) DeleteMute(ctx context.Context, command connector.MuteDeleteCommand) (connector.MuteDeleted, error) {
	if h.mutes == nil {
		return connector.MuteDeleted{}, fmt.Errorf("mute repository is not configured")
	}
	muter, err := h.repo.FindLocalByID(ctx, strings.TrimSpace(command.ActorID))
	if err != nil {
		return connector.MuteDeleted{}, err
	}
	if muter == nil {
		return connector.MuteDeleted{}, fmt.Errorf("local muter not found: %s", command.ActorID)
	}
	mutee, err := h.resolveMuteTarget(ctx, command.Target)
	if err != nil {
		return connector.MuteDeleted{}, fmt.Errorf("resolve mute target: %w", err)
	}
	if err := h.mutes.Delete(ctx, muter.ID, mutee.ID); err != nil {
		return connector.MuteDeleted{}, err
	}
	return connector.MuteDeleted{MuteeID: mutee.ID}, nil
}

func (h *Handler) resolveMuteTarget(ctx context.Context, target string) (*actors.Actor, error) {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		actor, err := h.repo.FindByURI(ctx, target)
		if err != nil || actor != nil {
			return actor, err
		}
	}
	actor, err := h.ResolveRemoteActor(ctx, target)
	if err != nil {
		return nil, err
	}
	if actor == nil {
		return nil, fmt.Errorf("mute target was not found")
	}
	return actor, nil
}

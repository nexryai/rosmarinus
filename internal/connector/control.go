package connector

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/nexryai/rosmarinus/internal/account"
)

const ControlAccountAuthorizationChanged = "account.authorization.changed"

type AccountReader interface {
	FindByID(context.Context, string) (*account.Account, error)
}

type OwnedActorLifecycle interface {
	ApplyAccountLifecycle(context.Context, string, account.Status, bool) (int64, error)
}

type AccountControlHandler struct {
	source   CommandSource
	accounts AccountReader
	actors   OwnedActorLifecycle
	logger   *log.Logger
}

type AccountAuthorizationChanged struct {
	AccountID     string `json:"account_id"`
	AuthzRevision int64  `json:"authz_revision"`
}

func NewAccountControlHandler(source CommandSource, accounts AccountReader, actors OwnedActorLifecycle, logger *log.Logger) *AccountControlHandler {
	return &AccountControlHandler{source: source, accounts: accounts, actors: actors, logger: logger}
}

func (h *AccountControlHandler) Subscribe(ctx context.Context) (func(), error) {
	if h == nil || h.source == nil {
		return func() {}, nil
	}
	return h.source.Subscribe(ctx, ControlAccountAuthorizationChanged, func(message CommandMessage) {
		if err := h.Handle(ctx, message); err != nil && h.logger != nil {
			h.logger.Printf("connector: account control failed message_id=%s err=%v", message.ID, err)
		}
	})
}

func (h *AccountControlHandler) Handle(ctx context.Context, message CommandMessage) error {
	if strings.TrimSpace(message.Name) != ControlAccountAuthorizationChanged {
		return fmt.Errorf("unknown account control event: %s", message.Name)
	}
	var event AccountAuthorizationChanged
	if err := decodeCommandData(message.Data, &event); err != nil {
		return err
	}
	event.AccountID = strings.TrimSpace(event.AccountID)
	if event.AccountID == "" {
		return fmt.Errorf("account control account_id is required")
	}
	if h.accounts == nil {
		return fmt.Errorf("Salvia account reader is not configured")
	}
	accountRecord, err := h.accounts.FindByID(ctx, event.AccountID)
	if err != nil {
		return fmt.Errorf("read Salvia account after control event: %w", err)
	}
	if h.actors == nil {
		return fmt.Errorf("owned actor lifecycle handler is not configured")
	}
	if accountRecord == nil {
		modified, err := h.actors.ApplyAccountLifecycle(ctx, event.AccountID, account.StatusSuspended, false)
		if err != nil {
			return fmt.Errorf("suspend actors for missing Salvia account: %w", err)
		}
		if h.logger != nil {
			h.logger.Printf("connector: suspended actors for missing Salvia account account_id=%s modified=%d", event.AccountID, modified)
		}
		return nil
	}
	deleted := accountRecord.DeletedAt != nil || accountRecord.Status == account.StatusDeleted
	modified, err := h.actors.ApplyAccountLifecycle(ctx, accountRecord.ID, accountRecord.Status, deleted)
	if err != nil {
		return fmt.Errorf("apply account actor lifecycle: %w", err)
	}
	if h.logger != nil {
		h.logger.Printf("connector: applied account actor lifecycle account_id=%s status=%s deleted=%t revision=%d modified=%d", accountRecord.ID, accountRecord.Status, deleted, accountRecord.AuthzRevision, modified)
	}
	return nil
}

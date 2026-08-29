package connector

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
)

type OwnedAccountLister interface {
	ListOwnedAccountIDs(context.Context) ([]string, error)
}

type AccountReconciler struct {
	accounts AccountReader
	owners   OwnedAccountLister
	actors   OwnedActorLifecycle
	logger   *log.Logger
}

func NewAccountReconciler(accounts AccountReader, owners OwnedAccountLister, actors OwnedActorLifecycle, logger *log.Logger) *AccountReconciler {
	return &AccountReconciler{accounts: accounts, owners: owners, actors: actors, logger: logger}
}

func (r *AccountReconciler) Reconcile(ctx context.Context) error {
	if r == nil || r.accounts == nil || r.owners == nil || r.actors == nil {
		return fmt.Errorf("account reconciler is not configured")
	}
	accountIDs, err := r.owners.ListOwnedAccountIDs(ctx)
	if err != nil {
		return fmt.Errorf("list owned accounts: %w", err)
	}
	for _, accountID := range accountIDs {
		accountRecord, err := r.accounts.FindByID(ctx, accountID)
		if err != nil {
			return fmt.Errorf("read Salvia account %s: %w", accountID, err)
		}
		status := account.StatusSuspended
		deleted := false
		if accountRecord != nil {
			status = accountRecord.Status
			deleted = accountRecord.DeletedAt != nil || accountRecord.Status == account.StatusDeleted
		}
		modified, err := r.actors.ApplyAccountLifecycle(ctx, accountID, status, deleted)
		if err != nil {
			return fmt.Errorf("apply actor lifecycle for account %s: %w", accountID, err)
		}
		if r.logger != nil && modified > 0 {
			r.logger.Printf("connector: reconciled account lifecycle account_id=%s status=%s deleted=%t modified=%d", accountID, status, deleted, modified)
		}
	}
	return nil
}

func (r *AccountReconciler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if err := r.Reconcile(ctx); err != nil && r.logger != nil {
		r.logger.Printf("connector: account reconciliation failed: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Reconcile(ctx); err != nil && r.logger != nil {
				r.logger.Printf("connector: account reconciliation failed: %v", err)
			}
		}
	}
}

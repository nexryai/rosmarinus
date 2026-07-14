package connector

import (
	"context"
	"testing"

	"github.com/nexryai/rosmarinus/internal/account"
)

type fakeOwnedAccountLister struct {
	accountIDs []string
}

func (f *fakeOwnedAccountLister) ListOwnedAccountIDs(ctx context.Context) ([]string, error) {
	_ = ctx
	return f.accountIDs, nil
}

type mapAccountReader struct {
	accounts map[string]*account.Account
}

func (m *mapAccountReader) FindByID(ctx context.Context, id string) (*account.Account, error) {
	_ = ctx
	return m.accounts[id], nil
}

type collectingActorSuspender struct {
	accountIDs []string
}

func (c *collectingActorSuspender) SuspendOwnedLocalActors(ctx context.Context, accountID string) (int64, error) {
	_ = ctx
	c.accountIDs = append(c.accountIDs, accountID)
	return 1, nil
}

func TestAccountReconcilerSuspendsMissingAndInactiveAccounts(t *testing.T) {
	suspender := &collectingActorSuspender{}
	reconciler := NewAccountReconciler(
		&mapAccountReader{accounts: map[string]*account.Account{
			"active":    {ID: "active", Status: account.StatusActive},
			"suspended": {ID: "suspended", Status: account.StatusSuspended},
		}},
		&fakeOwnedAccountLister{accountIDs: []string{"active", "suspended", "missing"}},
		suspender,
		nil,
	)
	if err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(suspender.accountIDs) != 2 || suspender.accountIDs[0] != "suspended" || suspender.accountIDs[1] != "missing" {
		t.Fatalf("suspended accounts = %+v", suspender.accountIDs)
	}
}

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

type lifecycleCall struct {
	accountID string
	status    account.Status
	deleted   bool
}

type collectingActorLifecycle struct {
	calls []lifecycleCall
}

func (c *collectingActorLifecycle) ApplyAccountLifecycle(ctx context.Context, accountID string, status account.Status, deleted bool) (int64, error) {
	_ = ctx
	c.calls = append(c.calls, lifecycleCall{accountID: accountID, status: status, deleted: deleted})
	return 1, nil
}

func TestAccountReconcilerAppliesCurrentAndMissingAccountStates(t *testing.T) {
	lifecycle := &collectingActorLifecycle{}
	reconciler := NewAccountReconciler(
		&mapAccountReader{accounts: map[string]*account.Account{
			"active":    {ID: "active", Status: account.StatusActive},
			"suspended": {ID: "suspended", Status: account.StatusSuspended},
		}},
		&fakeOwnedAccountLister{accountIDs: []string{"active", "suspended", "missing"}},
		lifecycle,
		nil,
	)
	if err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	want := []lifecycleCall{
		{accountID: "active", status: account.StatusActive},
		{accountID: "suspended", status: account.StatusSuspended},
		{accountID: "missing", status: account.StatusSuspended},
	}
	if len(lifecycle.calls) != len(want) {
		t.Fatalf("lifecycle calls = %+v", lifecycle.calls)
	}
	for i := range want {
		if lifecycle.calls[i] != want[i] {
			t.Fatalf("lifecycle call %d = %+v, want %+v", i, lifecycle.calls[i], want[i])
		}
	}
}

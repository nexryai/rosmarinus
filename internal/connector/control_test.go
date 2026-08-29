package connector

import (
	"context"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
)

type fakeAccountReader struct {
	account *account.Account
}

func (f *fakeAccountReader) FindByID(ctx context.Context, id string) (*account.Account, error) {
	_ = ctx
	if f.account != nil && f.account.ID == id {
		return f.account, nil
	}
	return nil, nil
}

type fakeActorLifecycle struct {
	accountID string
	status    account.Status
	deleted   bool
}

func (f *fakeActorLifecycle) ApplyAccountLifecycle(ctx context.Context, accountID string, status account.Status, deleted bool) (int64, error) {
	_ = ctx
	f.accountID = accountID
	f.status = status
	f.deleted = deleted
	return 2, nil
}

func TestAccountControlSuspendsActorsFromDatabaseState(t *testing.T) {
	lifecycle := &fakeActorLifecycle{}
	handler := NewAccountControlHandler(nil, &fakeAccountReader{account: &account.Account{
		ID:            "account-1",
		Status:        account.StatusSuspended,
		AuthzRevision: 4,
	}}, lifecycle, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: AccountAuthorizationChanged{AccountID: "account-1", AuthzRevision: 3},
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if lifecycle.accountID != "account-1" || lifecycle.status != account.StatusSuspended || lifecycle.deleted {
		t.Fatalf("lifecycle call = %+v", lifecycle)
	}
}

func TestAccountControlResumesActiveAccountActors(t *testing.T) {
	lifecycle := &fakeActorLifecycle{}
	handler := NewAccountControlHandler(nil, &fakeAccountReader{account: &account.Account{ID: "account-1", Status: account.StatusActive}}, lifecycle, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: AccountAuthorizationChanged{AccountID: "account-1"},
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if lifecycle.accountID != "account-1" || lifecycle.status != account.StatusActive || lifecycle.deleted {
		t.Fatalf("lifecycle call = %+v", lifecycle)
	}
}

func TestAccountControlSuspendsActorsForMissingDatabaseAccount(t *testing.T) {
	lifecycle := &fakeActorLifecycle{}
	handler := NewAccountControlHandler(nil, &fakeAccountReader{}, lifecycle, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: map[string]any{"account_id": "missing", "status": "active"},
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if lifecycle.accountID != "missing" || lifecycle.status != account.StatusSuspended || lifecycle.deleted {
		t.Fatalf("lifecycle call = %+v", lifecycle)
	}
}

func TestAccountControlPermanentlyDeletesActorsForDeletedAccount(t *testing.T) {
	deletedAt := time.Now().UTC()
	lifecycle := &fakeActorLifecycle{}
	handler := NewAccountControlHandler(nil, &fakeAccountReader{account: &account.Account{
		ID: "account-1", Status: account.StatusActive, DeletedAt: &deletedAt,
	}}, lifecycle, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: AccountAuthorizationChanged{AccountID: "account-1"},
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if lifecycle.accountID != "account-1" || lifecycle.status != account.StatusActive || !lifecycle.deleted {
		t.Fatalf("lifecycle call = %+v", lifecycle)
	}
}

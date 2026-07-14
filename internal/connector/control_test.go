package connector

import (
	"context"
	"testing"

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

type fakeActorSuspender struct {
	accountID string
}

func (f *fakeActorSuspender) SuspendOwnedLocalActors(ctx context.Context, accountID string) (int64, error) {
	_ = ctx
	f.accountID = accountID
	return 2, nil
}

func TestAccountControlSuspendsActorsFromDatabaseState(t *testing.T) {
	suspender := &fakeActorSuspender{}
	handler := NewAccountControlHandler(nil, &fakeAccountReader{account: &account.Account{
		ID:            "account-1",
		Status:        account.StatusSuspended,
		AuthzRevision: 4,
	}}, suspender, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: AccountAuthorizationChanged{AccountID: "account-1", AuthzRevision: 3},
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if suspender.accountID != "account-1" {
		t.Fatalf("suspended account = %q", suspender.accountID)
	}
}

func TestAccountControlDoesNotSuspendActiveAccount(t *testing.T) {
	suspender := &fakeActorSuspender{}
	handler := NewAccountControlHandler(nil, &fakeAccountReader{account: &account.Account{ID: "account-1", Status: account.StatusActive}}, suspender, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: AccountAuthorizationChanged{AccountID: "account-1"},
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if suspender.accountID != "" {
		t.Fatalf("active account was suspended: %q", suspender.accountID)
	}
}

func TestAccountControlRejectsPayloadStatusWithoutDatabaseAccount(t *testing.T) {
	handler := NewAccountControlHandler(nil, &fakeAccountReader{}, &fakeActorSuspender{}, nil)
	if err := handler.Handle(context.Background(), CommandMessage{
		Name: ControlAccountAuthorizationChanged,
		Data: map[string]any{"account_id": "missing", "status": "active"},
	}); err == nil {
		t.Fatal("expected missing database account to fail")
	}
}

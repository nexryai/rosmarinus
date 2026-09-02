package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/nexryai/rosmarinus/internal/account"
)

type fakePasskeyAccounts struct {
	reserved account.Account
	expires  time.Time
	err      error
}

func (s *fakePasskeyAccounts) ReserveInitial(_ context.Context, value account.Account, expires time.Time) (*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.reserved, s.expires = value, expires
	return &User{Account: value}, nil
}

func (*fakePasskeyAccounts) FindPendingByID(context.Context, string, time.Time) (*User, error) {
	return nil, nil
}

func (*fakePasskeyAccounts) ActivateInitial(context.Context, string, webauthn.Credential, time.Time) error {
	return nil
}

func (*fakePasskeyAccounts) DeletePending(context.Context, string) error { return nil }

func (*fakePasskeyAccounts) FindActiveByUserHandle(context.Context, []byte) (*User, error) {
	return nil, nil
}

func (*fakePasskeyAccounts) UpdateCredential(context.Context, string, webauthn.Credential, webauthn.Credential, time.Time) error {
	return nil
}

type fakeCeremonies struct {
	ceremony *Ceremony
}

func (s *fakeCeremonies) Create(_ context.Context, ceremony Ceremony) error {
	s.ceremony = &ceremony
	return nil
}

func (s *fakeCeremonies) Consume(_ context.Context, id string, ceremonyType CeremonyType, now time.Time) (*Ceremony, error) {
	if s.ceremony == nil || s.ceremony.ID != id || s.ceremony.Type != ceremonyType || !s.ceremony.ExpiresAt.After(now) {
		return nil, nil
	}
	ceremony := s.ceremony
	s.ceremony = nil
	return ceremony, nil
}

func TestPasskeyServiceBeginsSoleInitialRegistration(t *testing.T) {
	accounts := &fakePasskeyAccounts{}
	ceremonies := &fakeCeremonies{}
	service, err := NewPasskeyService(&webauthn.Config{
		RPID: "example.test", RPDisplayName: "Rosmarinus",
		RPOrigins: []string{"https://example.test"},
	}, accounts, ceremonies, nil, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	ids := []string{"account-id", "ceremony-id"}
	service.random = func(int) (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	options, err := service.BeginInitialRegistration(context.Background(), "admin", "Administrator")
	if err != nil {
		t.Fatal(err)
	}
	if options.CeremonyID != "ceremony-id" || options.PublicKey == nil {
		t.Fatalf("options = %+v", options)
	}
	if accounts.reserved.ID != "account-id" || accounts.reserved.Username != "admin" || len(accounts.reserved.WebAuthnID) != 32 || accounts.reserved.Status != account.StatusPending {
		t.Fatalf("reserved account = %+v", accounts.reserved)
	}
	if ceremonies.ceremony == nil || ceremonies.ceremony.AccountID != "account-id" || ceremonies.ceremony.Type != CeremonyInitialRegistration {
		t.Fatalf("ceremony = %+v", ceremonies.ceremony)
	}
	if !accounts.expires.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("expiry = %s", accounts.expires)
	}
}

func TestPasskeyServicePreservesRegistrationClosed(t *testing.T) {
	accounts := &fakePasskeyAccounts{err: ErrRegistrationClosed}
	service, err := NewPasskeyService(&webauthn.Config{
		RPID: "example.test", RPDisplayName: "Rosmarinus",
		RPOrigins: []string{"https://example.test"},
	}, accounts, &fakeCeremonies{}, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginInitialRegistration(context.Background(), "admin", ""); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("error = %v", err)
	}
}

func TestPasskeyCeremonyIsConsumedOnce(t *testing.T) {
	now := time.Now().UTC()
	ceremonies := &fakeCeremonies{ceremony: &Ceremony{ID: "ceremony", Type: CeremonyLogin, ExpiresAt: now.Add(time.Minute)}}
	service := &PasskeyService{ceremonies: ceremonies, now: func() time.Time { return now }}
	if _, err := service.consume(context.Background(), "ceremony", CeremonyLogin); err != nil {
		t.Fatal(err)
	}
	if _, err := service.consume(context.Background(), "ceremony", CeremonyLogin); !errors.Is(err, ErrCeremonyNotFound) {
		t.Fatalf("second consume error = %v", err)
	}
}

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
)

type memorySessions struct {
	items map[string]Session
}

func (r *memorySessions) Create(_ context.Context, session Session) error {
	if r.items == nil {
		r.items = make(map[string]Session)
	}
	r.items[session.TokenHash] = session
	return nil
}

func (r *memorySessions) FindActiveByTokenHash(_ context.Context, hash string, now time.Time) (*Session, error) {
	session, ok := r.items[hash]
	if !ok || session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return nil, nil
	}
	return &session, nil
}

func (r *memorySessions) RevokeByTokenHash(_ context.Context, hash string, now time.Time) error {
	session, ok := r.items[hash]
	if ok {
		session.RevokedAt = &now
		r.items[hash] = session
	}
	return nil
}

type memoryAccounts struct {
	account *account.Account
	err     error
}

func (r memoryAccounts) FindByID(_ context.Context, id string) (*account.Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.account == nil || r.account.ID != id {
		return nil, nil
	}
	return r.account, nil
}

func TestSessionManagerCreatesHashedSessionAndAuthenticates(t *testing.T) {
	repository := &memorySessions{}
	manager := NewSessionManager(repository, memoryAccounts{account: activeAccount()}, "session", time.Hour, true)
	manager.now = func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) }
	values := []string{"raw-session-token", "csrf-token", "session-id"}
	manager.random = func(int) (string, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	}

	credentials, err := manager.Create(context.Background(), "account-1")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token != "raw-session-token" || credentials.CSRFToken != "csrf-token" {
		t.Fatalf("credentials = %+v", credentials)
	}
	if _, ok := repository.items["raw-session-token"]; ok {
		t.Fatal("raw session token was stored")
	}
	stored, ok := repository.items[tokenHash("raw-session-token")]
	if !ok || stored.CSRFToken != "csrf-token" || stored.AccountID != "account-1" {
		t.Fatalf("stored session = %+v", stored)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: credentials.Token})
	accountID, csrf, err := manager.Authenticate(req)
	if err != nil || accountID != "account-1" || csrf != "csrf-token" {
		t.Fatalf("authenticate = account:%q csrf:%q err:%v", accountID, csrf, err)
	}
}

func TestSessionManagerRejectsInactiveAccount(t *testing.T) {
	repository := &memorySessions{}
	manager := NewSessionManager(repository, memoryAccounts{account: &account.Account{ID: "account-1", Status: account.StatusSuspended}}, "session", time.Hour, false)
	if _, err := manager.Create(context.Background(), "account-1"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v", err)
	}
}

func TestSessionManagerRevokesSession(t *testing.T) {
	repository := &memorySessions{}
	manager := NewSessionManager(repository, memoryAccounts{account: activeAccount()}, "session", time.Hour, false)
	credentials, err := manager.Create(context.Background(), "account-1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: credentials.Token})
	if err := manager.Revoke(req); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Authenticate(req); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("authenticate error = %v", err)
	}
}

func TestSessionCookieSecurityAttributes(t *testing.T) {
	manager := NewSessionManager(&memorySessions{}, memoryAccounts{account: activeAccount()}, "session", time.Hour, true)
	manager.now = func() time.Time { return time.Unix(1000, 0) }
	recorder := httptest.NewRecorder()
	manager.SetCookie(recorder, SessionCredentials{Token: "secret", ExpiresAt: time.Unix(4600, 0)})
	result := recorder.Result()
	cookies := result.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie = %+v", cookie)
	}
}

func activeAccount() *account.Account {
	return &account.Account{ID: "account-1", Status: account.StatusActive}
}

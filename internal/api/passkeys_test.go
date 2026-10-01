package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

type fakePasskeyManager struct {
	accountID  string
	name       string
	passkeyID  string
	ceremonyID string
	deleteErr  error
}

func (f *fakePasskeyManager) ListPasskeys(_ context.Context, accountID string) ([]appauth.PasskeyInfo, error) {
	f.accountID = accountID
	return []appauth.PasskeyInfo{{ID: "key-1", Name: "Phone"}}, nil
}
func (f *fakePasskeyManager) BeginAdditionalRegistration(_ context.Context, accountID, name string) (appauth.CeremonyOptions, error) {
	f.accountID, f.name = accountID, name
	return appauth.CeremonyOptions{CeremonyID: "ceremony", PublicKey: map[string]any{"challenge": "challenge"}}, nil
}
func (f *fakePasskeyManager) FinishAdditionalRegistration(_ context.Context, accountID, ceremonyID string, _ *http.Request) error {
	f.accountID, f.ceremonyID = accountID, ceremonyID
	return nil
}
func (f *fakePasskeyManager) RenamePasskey(_ context.Context, accountID, passkeyID, name string) error {
	f.accountID, f.passkeyID, f.name = accountID, passkeyID, name
	return nil
}
func (f *fakePasskeyManager) DeletePasskey(_ context.Context, accountID, passkeyID string) error {
	f.accountID, f.passkeyID = accountID, passkeyID
	return f.deleteErr
}

func passkeyHandler(authenticator Authenticator, manager PasskeyManager) http.Handler {
	return NewHandlerCompleteWithMediaProxy(authenticator, nil, nil, nil, nil, nil, InstanceInfo{}, nil, nil, nil, nil, nil, nil, 0, nil, nil, time.Hour, HandlerExtras{Passkeys: manager})
}

func TestPasskeyManagementRequiresSessionAndCSRF(t *testing.T) {
	manager := &fakePasskeyManager{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/passkeys", nil)
	response := httptest.NewRecorder()
	passkeyHandler(fakeAuthenticator{}, manager).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || manager.accountID != "" {
		t.Fatalf("unauthorized status=%d account=%q", response.Code, manager.accountID)
	}

	handler := passkeyHandler(fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf"}}, manager)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/passkeys", nil))
	if response.Code != http.StatusOK || manager.accountID != "account-1" {
		t.Fatalf("list status=%d account=%q", response.Code, manager.accountID)
	}
	manager.accountID = ""
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, jsonRequest(http.MethodPost, "/api/v1/passkeys/register/start", `{"name":"Phone"}`))
	if response.Code != http.StatusForbidden || manager.accountID != "" {
		t.Fatalf("start without CSRF status=%d account=%q", response.Code, manager.accountID)
	}
	request = jsonRequest(http.MethodPost, "/api/v1/passkeys/register/start", `{"name":"Phone"}`)
	request.Header.Set("X-CSRF-Token", "csrf")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || manager.accountID != "account-1" || manager.name != "Phone" {
		t.Fatalf("start status=%d account=%q name=%q", response.Code, manager.accountID, manager.name)
	}
}

func TestPasskeyDeleteRefusesLastCredential(t *testing.T) {
	manager := &fakePasskeyManager{deleteErr: appauth.ErrLastPasskey}
	handler := passkeyHandler(fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf"}}, manager)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/passkeys/key-1", nil)
	request.Header.Set("X-CSRF-Token", "csrf")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertError(t, response, http.StatusConflict, "last_passkey")
	if manager.accountID != "account-1" || manager.passkeyID != "key-1" {
		t.Fatalf("scope = %q/%q", manager.accountID, manager.passkeyID)
	}
	manager.deleteErr = errors.New("database unavailable")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertError(t, response, http.StatusInternalServerError, "internal_error")
}

func TestPasskeyRegistrationFinishRequiresCeremonyHeader(t *testing.T) {
	manager := &fakePasskeyManager{}
	handler := passkeyHandler(fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf"}}, manager)
	request := jsonRequest(http.MethodPost, "/api/v1/passkeys/register/finish", `{}`)
	request.Header.Set("X-CSRF-Token", "csrf")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertError(t, response, http.StatusBadRequest, "ceremony_required")
	if manager.ceremonyID != "" {
		t.Fatal("finish was called without a ceremony")
	}

	request = jsonRequest(http.MethodPost, "/api/v1/passkeys/register/finish", `{}`)
	request.Header.Set("X-CSRF-Token", "csrf")
	request.Header.Set("X-WebAuthn-Ceremony-ID", "ceremony")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || manager.accountID != "account-1" || manager.ceremonyID != "ceremony" {
		t.Fatalf("finish status=%d account=%q ceremony=%q", response.Code, manager.accountID, manager.ceremonyID)
	}
}

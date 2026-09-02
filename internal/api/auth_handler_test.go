package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

type fakePasskeyFlow struct {
	beginSetup appauth.CeremonyOptions
	beginLogin appauth.CeremonyOptions
	finish     appauth.SessionCredentials
	err        error
}

func (f *fakePasskeyFlow) BeginInitialRegistration(context.Context, string, string) (appauth.CeremonyOptions, error) {
	return f.beginSetup, f.err
}

func (f *fakePasskeyFlow) FinishInitialRegistration(context.Context, string, *http.Request) (appauth.SessionCredentials, error) {
	return f.finish, f.err
}

func (f *fakePasskeyFlow) BeginLogin(context.Context) (appauth.CeremonyOptions, error) {
	return f.beginLogin, f.err
}

func (f *fakePasskeyFlow) FinishLogin(context.Context, string, *http.Request) (appauth.SessionCredentials, error) {
	return f.finish, f.err
}

type fakeSessionController struct {
	accountID string
	csrf      string
	revoked   bool
	set       appauth.SessionCredentials
}

func (s *fakeSessionController) Authenticate(*http.Request) (string, string, error) {
	if s.accountID == "" {
		return "", "", appauth.ErrUnauthenticated
	}
	return s.accountID, s.csrf, nil
}

func (s *fakeSessionController) Revoke(*http.Request) error {
	s.revoked = true
	return nil
}

func (s *fakeSessionController) SetCookie(_ http.ResponseWriter, credentials appauth.SessionCredentials) {
	s.set = credentials
}

func (s *fakeSessionController) ClearCookie(http.ResponseWriter) {}

type fakeInstallation struct {
	active bool
	err    error
}

func (s fakeInstallation) HasActive(context.Context) (bool, error) { return s.active, s.err }

func TestAuthHandlerReportsSetupStatus(t *testing.T) {
	handler := NewAuthHandler(&fakePasskeyFlow{}, &fakeSessionController{}, fakeInstallation{}, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/setup", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"data\":{\"setup_required\":true}}\n" {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthHandlerBeginsInitialRegistration(t *testing.T) {
	flow := &fakePasskeyFlow{beginSetup: appauth.CeremonyOptions{CeremonyID: "ceremony-1", PublicKey: map[string]string{"challenge": "challenge"}}}
	handler := NewAuthHandler(flow, &fakeSessionController{}, fakeInstallation{}, nil)
	recorder := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/api/v1/auth/setup/start", `{"username":"admin","display_name":"Administrator"}`)
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated || !bytesContains(recorder.Body.String(), `"ceremony_id":"ceremony-1"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthHandlerRejectsReplayedCeremony(t *testing.T) {
	flow := &fakePasskeyFlow{err: appauth.ErrCeremonyNotFound}
	handler := NewAuthHandler(flow, &fakeSessionController{}, fakeInstallation{}, nil)
	recorder := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/api/v1/auth/login/finish", `{}`)
	req.Header.Set("X-WebAuthn-Ceremony-ID", "ceremony-used")
	handler.ServeHTTP(recorder, req)
	assertError(t, recorder, http.StatusBadRequest, "ceremony_expired")
}

func TestAuthHandlerSetsSessionAfterLogin(t *testing.T) {
	credentials := appauth.SessionCredentials{Token: "session-token", CSRFToken: "csrf-token"}
	flow := &fakePasskeyFlow{finish: credentials}
	sessions := &fakeSessionController{}
	handler := NewAuthHandler(flow, sessions, fakeInstallation{active: true}, nil)
	recorder := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/api/v1/auth/login/finish", `{}`)
	req.Header.Set("X-WebAuthn-Ceremony-ID", "ceremony-1")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || sessions.set.Token != "session-token" {
		t.Fatalf("response=%d set=%+v body=%s", recorder.Code, sessions.set, recorder.Body.String())
	}
}

func TestAuthHandlerLogoutRequiresSessionAndCSRF(t *testing.T) {
	sessions := &fakeSessionController{accountID: "account-1", csrf: "csrf-token"}
	handler := NewAuthHandler(&fakePasskeyFlow{}, sessions, fakeInstallation{active: true}, nil)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	handler.ServeHTTP(recorder, req)
	assertError(t, recorder, http.StatusForbidden, "csrf_failed")

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("X-CSRF-Token", "csrf-token")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || !sessions.revoked {
		t.Fatalf("response=%d revoked=%v", recorder.Code, sessions.revoked)
	}
}

func TestAuthHandlerHidesPasskeyErrors(t *testing.T) {
	flow := &fakePasskeyFlow{err: errors.New("private credential material")}
	handler := NewAuthHandler(flow, &fakeSessionController{}, fakeInstallation{}, nil)
	recorder := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/api/v1/auth/login/finish", `{}`)
	req.Header.Set("X-WebAuthn-Ceremony-ID", "ceremony-1")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized || bytesContains(recorder.Body.String(), "private credential") {
		t.Fatalf("response=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func bytesContains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

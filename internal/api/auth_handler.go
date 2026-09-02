package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"

	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

type PasskeyFlow interface {
	BeginInitialRegistration(context.Context, string, string) (appauth.CeremonyOptions, error)
	FinishInitialRegistration(context.Context, string, *http.Request) (appauth.SessionCredentials, error)
	BeginLogin(context.Context) (appauth.CeremonyOptions, error)
	FinishLogin(context.Context, string, *http.Request) (appauth.SessionCredentials, error)
}

type SessionController interface {
	Authenticator
	Revoke(*http.Request) error
	SetCookie(http.ResponseWriter, appauth.SessionCredentials)
	ClearCookie(http.ResponseWriter)
}

type InstallationStore interface {
	HasActive(context.Context) (bool, error)
}

type AuthHandler struct {
	passkeys     PasskeyFlow
	sessions     SessionController
	installation InstallationStore
	logger       *log.Logger
}

func NewAuthHandler(passkeys PasskeyFlow, sessions SessionController, installation InstallationStore, logger *log.Logger) http.Handler {
	return &AuthHandler{passkeys: passkeys, sessions: sessions, installation: installation, logger: logger}
}

func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/v1/auth/setup" && r.Method == http.MethodGet:
		h.setupStatus(w, r)
	case path == "/api/v1/auth/setup/start" && r.Method == http.MethodPost:
		h.beginSetup(w, r)
	case path == "/api/v1/auth/setup/finish" && r.Method == http.MethodPost:
		h.finishSetup(w, r)
	case path == "/api/v1/auth/login/start" && r.Method == http.MethodPost:
		h.beginLogin(w, r)
	case path == "/api/v1/auth/login/finish" && r.Method == http.MethodPost:
		h.finishLogin(w, r)
	case path == "/api/v1/auth/logout" && r.Method == http.MethodPost:
		h.logout(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (h *AuthHandler) setupStatus(w http.ResponseWriter, r *http.Request) {
	if h.installation == nil {
		h.internalError(w, r, fmt.Errorf("installation store is not configured"))
		return
	}
	active, err := h.installation.HasActive(r.Context())
	if err != nil {
		h.internalError(w, r, fmt.Errorf("read installation status: %w", err))
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"setup_required": !active}})
}

func (h *AuthHandler) beginSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	if !decodeAPIJSON(w, r, &body) {
		return
	}
	options, err := h.passkeys.BeginInitialRegistration(r.Context(), body.Username, body.DisplayName)
	if errors.Is(err, appauth.ErrRegistrationClosed) {
		writeAPIError(w, http.StatusConflict, "registration_closed", "initial registration is closed")
		return
	}
	if err != nil {
		h.logError(r, err)
		writeAPIError(w, http.StatusUnprocessableEntity, "registration_failed", "passkey registration could not be started")
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"data": options})
}

func (h *AuthHandler) finishSetup(w http.ResponseWriter, r *http.Request) {
	if !prepareWebAuthnBody(w, r) {
		return
	}
	credentials, err := h.passkeys.FinishInitialRegistration(r.Context(), r.Header.Get("X-WebAuthn-Ceremony-ID"), r)
	if err != nil {
		h.passkeyFailure(w, r, err, "registration_failed")
		return
	}
	h.sessions.SetCookie(w, credentials)
	writeAPIJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"csrf_token": credentials.CSRFToken}})
}

func (h *AuthHandler) beginLogin(w http.ResponseWriter, r *http.Request) {
	options, err := h.passkeys.BeginLogin(r.Context())
	if err != nil {
		h.internalError(w, r, fmt.Errorf("begin login: %w", err))
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"data": options})
}

func (h *AuthHandler) finishLogin(w http.ResponseWriter, r *http.Request) {
	if !prepareWebAuthnBody(w, r) {
		return
	}
	credentials, err := h.passkeys.FinishLogin(r.Context(), r.Header.Get("X-WebAuthn-Ceremony-ID"), r)
	if err != nil {
		h.passkeyFailure(w, r, err, "authentication_failed")
		return
	}
	h.sessions.SetCookie(w, credentials)
	writeAPIJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"csrf_token": credentials.CSRFToken}})
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	accountID, csrfToken, err := h.sessions.Authenticate(r)
	if err != nil || accountID == "" {
		writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !validCSRF(r.Header.Get("X-CSRF-Token"), csrfToken) {
		writeAPIError(w, http.StatusForbidden, "csrf_failed", "CSRF token is missing or invalid")
		return
	}
	if err := h.sessions.Revoke(r); err != nil {
		h.internalError(w, r, err)
		return
	}
	h.sessions.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) passkeyFailure(w http.ResponseWriter, r *http.Request, err error, code string) {
	h.logError(r, err)
	if errors.Is(err, appauth.ErrCeremonyNotFound) {
		writeAPIError(w, http.StatusBadRequest, "ceremony_expired", "WebAuthn ceremony is missing, expired, or already used")
		return
	}
	writeAPIError(w, http.StatusUnauthorized, code, "passkey verification failed")
}

func (h *AuthHandler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logError(r, err)
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func (h *AuthHandler) logError(r *http.Request, err error) {
	if h.logger != nil {
		h.logger.Printf("api: authentication request failed method=%s path=%s err=%v", r.Method, r.URL.Path, err)
	}
}

func prepareWebAuthnBody(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-WebAuthn-Ceremony-ID")) == "" {
		writeAPIError(w, http.StatusBadRequest, "ceremony_required", "X-WebAuthn-Ceremony-ID is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	return true
}

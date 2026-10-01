package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

func (h *Handler) passkeyResource(w http.ResponseWriter, r *http.Request, accountID string, segments []string) {
	if h.passkeys == nil {
		h.internalError(w, r, fmt.Errorf("passkey manager is not configured"))
		return
	}
	switch {
	case len(segments) == 0 && r.Method == http.MethodGet:
		passkeys, err := h.passkeys.ListPasskeys(r.Context(), accountID)
		if err != nil {
			if errors.Is(err, appauth.ErrUnauthenticated) {
				h.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			h.internalError(w, r, err)
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"data": passkeys})
	case len(segments) == 2 && segments[0] == "register" && segments[1] == "start" && r.Method == http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if !h.decodeJSON(w, r, &body, false) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" || len([]rune(body.Name)) > 64 {
			h.writeError(w, http.StatusBadRequest, "invalid_name", "passkey name must contain 1 to 64 characters")
			return
		}
		options, err := h.passkeys.BeginAdditionalRegistration(r.Context(), accountID, body.Name)
		if err != nil {
			if errors.Is(err, appauth.ErrUnauthenticated) {
				h.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			h.internalError(w, r, err)
			return
		}
		h.writeJSON(w, http.StatusCreated, map[string]any{"data": options})
	case len(segments) == 2 && segments[0] == "register" && segments[1] == "finish" && r.Method == http.MethodPost:
		if !prepareWebAuthnBody(w, r) {
			return
		}
		err := h.passkeys.FinishAdditionalRegistration(r.Context(), accountID, r.Header.Get("X-WebAuthn-Ceremony-ID"), r)
		if errors.Is(err, appauth.ErrCeremonyNotFound) {
			h.writeError(w, http.StatusBadRequest, "ceremony_expired", "WebAuthn ceremony is missing, expired, or already used")
			return
		}
		if errors.Is(err, appauth.ErrPasskeyExists) {
			h.writeError(w, http.StatusConflict, "passkey_exists", "passkey is already registered")
			return
		}
		if errors.Is(err, appauth.ErrPasskeyVerification) || errors.Is(err, appauth.ErrUnauthenticated) {
			if h.logger != nil {
				h.logger.Printf("api: additional passkey registration failed account=%s err=%v", accountID, err)
			}
			h.writeError(w, http.StatusUnauthorized, "registration_failed", "passkey verification failed")
			return
		}
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		passkeys, err := h.passkeys.ListPasskeys(r.Context(), accountID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		h.writeJSON(w, http.StatusCreated, map[string]any{"data": passkeys})
	case len(segments) == 1 && segments[0] != "register" && r.Method == http.MethodPatch:
		var body struct {
			Name string `json:"name"`
		}
		if !h.decodeJSON(w, r, &body, false) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" || len([]rune(body.Name)) > 64 {
			h.writeError(w, http.StatusBadRequest, "invalid_name", "passkey name must contain 1 to 64 characters")
			return
		}
		if err := h.passkeys.RenamePasskey(r.Context(), accountID, segments[0], body.Name); err != nil {
			if errors.Is(err, appauth.ErrPasskeyNotFound) {
				h.writeError(w, http.StatusNotFound, "passkey_not_found", "passkey not found")
				return
			}
			h.internalError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(segments) == 1 && segments[0] != "register" && r.Method == http.MethodDelete:
		if err := h.passkeys.DeletePasskey(r.Context(), accountID, segments[0]); err != nil {
			if errors.Is(err, appauth.ErrLastPasskey) {
				h.writeError(w, http.StatusConflict, "last_passkey", "the last passkey cannot be deleted")
				return
			}
			if errors.Is(err, appauth.ErrPasskeyNotFound) {
				h.writeError(w, http.StatusNotFound, "passkey_not_found", "passkey not found")
				return
			}
			h.internalError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
	}
}

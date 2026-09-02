package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/settings"
)

type InstanceInfo struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	URL         string `json:"url"`
	Version     string `json:"version"`
	PasskeyOnly bool   `json:"passkey_only"`
}

func NewInstanceInfo(name, publicURL, version string) InstanceInfo {
	host := ""
	if parsed, err := url.Parse(publicURL); err == nil {
		host = parsed.Host
	}
	version = strings.TrimPrefix(strings.TrimSpace(version), "rosmarinus/")
	return InstanceInfo{Name: name, Host: host, URL: strings.TrimRight(publicURL, "/"), Version: version, PasskeyOnly: true}
}

func (h *Handler) accountSettings(w http.ResponseWriter, r *http.Request, accountID string) {
	if h.settings == nil {
		h.internalError(w, r, fmt.Errorf("settings store is not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		value, err := h.settings.GetAccount(r.Context(), accountID)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("get account settings: %w", err))
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"data": projectAccountSettings(value)})
	case http.MethodPatch:
		var body struct {
			Theme           *string `json:"theme"`
			ReduceMotion    *bool   `json:"reduce_motion"`
			CompactMode     *bool   `json:"compact_mode"`
			SelectedActorID *string `json:"selected_actor_id"`
		}
		if !h.decodeJSON(w, r, &body, false) {
			return
		}
		if body.Theme != nil && !validTheme(*body.Theme) {
			h.writeError(w, http.StatusUnprocessableEntity, "invalid_theme", "theme is not supported")
			return
		}
		if body.SelectedActorID != nil && *body.SelectedActorID != "" {
			if _, ok := h.authorizeActor(w, r, accountID, *body.SelectedActorID, false); !ok {
				return
			}
		}
		if body.Theme == nil && body.ReduceMotion == nil && body.CompactMode == nil && body.SelectedActorID == nil {
			h.writeError(w, http.StatusUnprocessableEntity, "empty_patch", "at least one setting is required")
			return
		}
		value, err := h.settings.UpdateAccount(r.Context(), accountID, settings.AccountPatch{
			Theme: body.Theme, ReduceMotion: body.ReduceMotion, CompactMode: body.CompactMode,
			SelectedActorID: body.SelectedActorID,
		})
		if err != nil {
			h.internalError(w, r, fmt.Errorf("update account settings: %w", err))
			return
		}
		h.publishProjectionInvalidation(r, accountID, "", "settings")
		h.writeJSON(w, http.StatusOK, map[string]any{"data": projectAccountSettings(value)})
	default:
		h.methodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (h *Handler) actorSettings(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if len(segments) != 0 {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if _, ok := h.authorizeActor(w, r, accountID, actorID, false); !ok {
		return
	}
	if h.settings == nil {
		h.internalError(w, r, fmt.Errorf("settings store is not configured"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		value, err := h.settings.GetActor(r.Context(), accountID, actorID)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("get Actor settings: %w", err))
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"data": projectActorSettings(value)})
	case http.MethodPatch:
		var body struct {
			DefaultVisibility  *string `json:"default_visibility"`
			ShowContentWarning *bool   `json:"show_content_warning"`
			DisplayOrder       *int    `json:"display_order"`
			Color              *string `json:"color"`
			Pinned             *bool   `json:"pinned"`
		}
		if !h.decodeJSON(w, r, &body, false) {
			return
		}
		if body.DefaultVisibility != nil && !validDefaultVisibility(*body.DefaultVisibility) {
			h.writeError(w, http.StatusUnprocessableEntity, "invalid_visibility", "default_visibility is not supported")
			return
		}
		if body.DisplayOrder != nil && (*body.DisplayOrder < -10000 || *body.DisplayOrder > 10000) {
			h.writeError(w, http.StatusUnprocessableEntity, "invalid_display_order", "display_order must be between -10000 and 10000")
			return
		}
		if body.Color != nil && !validActorColor(*body.Color) {
			h.writeError(w, http.StatusUnprocessableEntity, "invalid_color", "color must be empty or a six-digit hexadecimal color")
			return
		}
		if body.DefaultVisibility == nil && body.ShowContentWarning == nil && body.DisplayOrder == nil && body.Color == nil && body.Pinned == nil {
			h.writeError(w, http.StatusUnprocessableEntity, "empty_patch", "at least one setting is required")
			return
		}
		value, err := h.settings.UpdateActor(r.Context(), accountID, actorID, settings.ActorPatch{
			DefaultVisibility: body.DefaultVisibility, ShowContentWarning: body.ShowContentWarning,
			DisplayOrder: body.DisplayOrder, Color: body.Color, Pinned: body.Pinned,
		})
		if err != nil {
			h.internalError(w, r, fmt.Errorf("update Actor settings: %w", err))
			return
		}
		h.publishProjectionInvalidation(r, accountID, actorID, "actor_settings")
		h.writeJSON(w, http.StatusOK, map[string]any{"data": projectActorSettings(value)})
	default:
		h.methodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (h *Handler) publishProjectionInvalidation(r *http.Request, accountID, actorID, resource string) {
	if h.events == nil {
		return
	}
	if err := h.events.Publish(r.Context(), accountID, "projection.invalidated", actorID, map[string]string{"resource": resource}); err != nil && h.logger != nil {
		h.logger.Printf("api: publish projection invalidation failed account_id=%s actor_id=%s resource=%s err=%v", accountID, actorID, resource, err)
	}
}

type accountSettingsView struct {
	Theme           string    `json:"theme"`
	ReduceMotion    bool      `json:"reduce_motion"`
	CompactMode     bool      `json:"compact_mode"`
	SelectedActorID string    `json:"selected_actor_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type actorSettingsView struct {
	ActorID            string    `json:"actor_id"`
	DefaultVisibility  string    `json:"default_visibility"`
	ShowContentWarning bool      `json:"show_content_warning"`
	DisplayOrder       int       `json:"display_order"`
	Color              string    `json:"color,omitempty"`
	Pinned             bool      `json:"pinned"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

func projectAccountSettings(value *settings.Account) accountSettingsView {
	if value == nil {
		value = &settings.Account{Theme: settings.DefaultTheme}
	}
	return accountSettingsView{
		Theme: value.Theme, ReduceMotion: value.ReduceMotion, CompactMode: value.CompactMode,
		SelectedActorID: value.SelectedActorID, UpdatedAt: value.UpdatedAt,
	}
}

func projectActorSettings(value *settings.Actor) actorSettingsView {
	if value == nil {
		value = &settings.Actor{DefaultVisibility: string(notes.VisibilityPublic), ShowContentWarning: true}
	}
	return actorSettingsView{
		ActorID: value.ActorID, DefaultVisibility: value.DefaultVisibility,
		ShowContentWarning: value.ShowContentWarning, DisplayOrder: value.DisplayOrder,
		Color: value.Color, Pinned: value.Pinned, UpdatedAt: value.UpdatedAt,
	}
}

func validTheme(theme string) bool {
	switch theme {
	case "yellow", "light", "dark", "system":
		return true
	default:
		return false
	}
}

func validDefaultVisibility(visibility string) bool {
	switch notes.Visibility(visibility) {
	case notes.VisibilityPublic, notes.VisibilityHome, notes.VisibilityFollowers:
		return true
	default:
		return false
	}
}

func validActorColor(color string) bool {
	if color == "" {
		return true
	}
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, character := range color[1:] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

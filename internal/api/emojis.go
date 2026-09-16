package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
)

func (h *Handler) emojiResource(w http.ResponseWriter, r *http.Request, accountID string, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.emojis(w, r)
		case http.MethodPost:
			h.createEmoji(w, r, accountID)
		default:
			h.methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}
	if len(segments) == 1 && segments[0] == "import" {
		h.importEmoji(w, r, accountID)
		return
	}
	if len(segments) != 1 {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		h.updateEmoji(w, r, accountID, segments[0])
	case http.MethodDelete:
		h.deleteEmoji(w, r, segments[0])
	default:
		h.methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (h *Handler) createEmoji(w http.ResponseWriter, r *http.Request, accountID string) {
	if h.emojiAdmin == nil {
		h.internalError(w, r, fmt.Errorf("emoji administration service is not configured"))
		return
	}
	var body struct {
		ActorID string `json:"actor_id"`
		Name    string `json:"name"`
		MediaID string `json:"media_id"`
	}
	if !h.decodeJSON(w, r, &body, false) {
		return
	}
	actor, ok := h.authorizeActor(w, r, accountID, strings.TrimSpace(body.ActorID), false)
	if !ok {
		return
	}
	created, err := h.emojiAdmin.CreateFromMedia(r.Context(), actor.ID, body.Name, body.MediaID)
	if err != nil {
		h.writeEmojiAdminError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, map[string]any{"data": h.projectEmoji(*created)})
}

func (h *Handler) updateEmoji(w http.ResponseWriter, r *http.Request, accountID, id string) {
	if h.emojiAdmin == nil {
		h.internalError(w, r, fmt.Errorf("emoji administration service is not configured"))
		return
	}
	var body struct {
		ActorID string `json:"actor_id"`
		Name    string `json:"name"`
		MediaID string `json:"media_id,omitempty"`
	}
	if !h.decodeJSON(w, r, &body, false) {
		return
	}
	actor, ok := h.authorizeActor(w, r, accountID, strings.TrimSpace(body.ActorID), false)
	if !ok {
		return
	}
	updated, err := h.emojiAdmin.Update(r.Context(), actor.ID, id, body.Name, body.MediaID)
	if err != nil {
		h.writeEmojiAdminError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": h.projectEmoji(*updated)})
}

func (h *Handler) deleteEmoji(w http.ResponseWriter, r *http.Request, id string) {
	if h.emojiAdmin == nil {
		h.internalError(w, r, fmt.Errorf("emoji administration service is not configured"))
		return
	}
	if err := h.emojiAdmin.Delete(r.Context(), id); err != nil {
		h.writeEmojiAdminError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"id": id}})
}

func (h *Handler) importEmoji(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, http.MethodPost)
		return
	}
	if h.emojiAdmin == nil {
		h.internalError(w, r, fmt.Errorf("emoji administration service is not configured"))
		return
	}
	var body struct {
		ActorID  string `json:"actor_id"`
		SourceID string `json:"source_id"`
		Name     string `json:"name,omitempty"`
	}
	if !h.decodeJSON(w, r, &body, false) {
		return
	}
	actor, ok := h.authorizeActor(w, r, accountID, strings.TrimSpace(body.ActorID), false)
	if !ok {
		return
	}
	created, err := h.emojiAdmin.ImportRemote(r.Context(), actor.ID, body.SourceID, body.Name)
	if err != nil {
		h.writeEmojiAdminError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, map[string]any{"data": h.projectEmoji(*created)})
}

func (h *Handler) writeEmojiAdminError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, emojis.ErrNameConflict):
		h.writeError(w, http.StatusConflict, "emoji_name_conflict", "an emoji with that name already exists")
	case errors.Is(err, emojis.ErrNotFound):
		h.writeError(w, http.StatusNotFound, "emoji_not_found", "emoji not found")
	default:
		if h.logger != nil {
			h.logger.Printf("api: emoji administration failed err=%v", err)
		}
		h.writeError(w, http.StatusUnprocessableEntity, "emoji_operation_failed", "emoji operation could not be completed")
	}
}

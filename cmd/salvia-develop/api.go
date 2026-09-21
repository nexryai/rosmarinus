package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// dummyAPI serves the read-only REST contract that Salvia consumes. Every
// response is derived from the immutable demoData; mutation requests return
// plausible success payloads without persisting anything.
type dummyAPI struct {
	data *demoData
}

func newDummyAPI(data *demoData) *dummyAPI {
	return &dummyAPI{data: data}
}

func (a *dummyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1"), "/")
	var segments []string
	if rest != "" {
		segments = strings.Split(rest, "/")
	}
	if len(segments) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}

	switch segments[0] {
	case "auth":
		a.handleAuth(w, r, segments)
	case "session":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeData(w, a.data.session)
	case "instance":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeData(w, instanceInfo{Name: "Salvia Development", Host: "127.0.0.1", URL: "http://127.0.0.1", Version: "dev", PasskeyOnly: true})
	case "events":
		a.handleEvents(w, r)
	case "actors":
		a.handleActors(w, r, segments)
	case "timelines":
		a.handleTimelines(w, r, segments)
	case "notes":
		a.handleNotes(w, r, segments)
	case "emojis":
		a.handleEmojis(w, r, segments)
	case "notifications":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writePage(w, a.data.notifications, "")
	case "profiles":
		a.handleProfiles(w, r, segments)
	case "settings":
		a.handleAccountSettings(w, r)
	case "dev-upload":
		if r.Method != http.MethodPut {
			methodNotAllowed(w, http.MethodPut)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (a *dummyAPI) handleAuth(w http.ResponseWriter, r *http.Request, segments []string) {
	switch {
	case r.Method == http.MethodGet && len(segments) == 2 && segments[1] == "setup":
		writeData(w, map[string]bool{"setup_required": false})
	case r.Method == http.MethodPost && len(segments) == 2 && segments[1] == "logout":
		w.WriteHeader(http.StatusNoContent)
	default:
		// The development session is always authenticated, so the WebAuthn
		// ceremony endpoints are intentionally unavailable.
		writeError(w, http.StatusNotImplemented, "not_implemented", "authentication ceremonies are disabled in development")
	}
}

func (a *dummyAPI) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (a *dummyAPI) handleActors(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) == 1 {
		switch r.Method {
		case http.MethodGet:
			writePage(w, a.data.actors, "")
		case http.MethodPost:
			writeData(w, map[string]any{})
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}

	actorID := segments[1]
	if len(segments) == 2 {
		switch r.Method {
		case http.MethodGet:
			if actor := a.data.findActor(actorID); actor != nil {
				writeData(w, actor)
				return
			}
			writeError(w, http.StatusNotFound, "not_found", "actor not found")
		case http.MethodPatch, http.MethodDelete:
			writeData(w, map[string]any{})
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
		return
	}

	switch segments[2] {
	case "settings":
		a.handleActorSettings(w, r, actorID)
	case "notifications":
		a.handleActorNotifications(w, r, actorID, segments)
	case "follow-requests":
		a.handleFollowRequests(w, r, actorID, segments)
	case "following":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writePage(w, a.connections(actorID), "")
	case "followers":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writePage(w, a.connections(actorID), "")
	case "antennas":
		a.handleAntennas(w, r, segments)
	case "posts":
		if len(segments) >= 4 && r.Method == http.MethodDelete {
			writeData(w, map[string]any{})
			return
		}
		if r.Method == http.MethodPost {
			writeData(w, map[string]any{})
			return
		}
		methodNotAllowed(w, http.MethodPost, http.MethodDelete)
	case "media":
		a.handleMedia(w, r, segments)
	case "poll-votes":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		writeData(w, map[string]any{})
	case "reactions":
		if r.Method != http.MethodPut && r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodPut, http.MethodDelete)
			return
		}
		writeData(w, map[string]any{})
	case "follows", "blocks", "mutes":
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodPost, http.MethodDelete)
			return
		}
		writeData(w, map[string]any{})
	case "profiles":
		if len(segments) >= 4 && segments[3] == "resolve" && r.Method == http.MethodPost {
			writeData(w, a.data.profileFor("dev-remote-1"))
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (a *dummyAPI) handleActorSettings(w http.ResponseWriter, r *http.Request, actorID string) {
	settings, ok := a.data.actorSettings[actorID]
	if !ok {
		settings = actorSettings{ActorID: actorID, DefaultVisibility: "public", UpdatedAt: demoCreatedAt}
	}
	switch r.Method {
	case http.MethodGet, http.MethodPatch:
		writeData(w, settings)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (a *dummyAPI) handleActorNotifications(w http.ResponseWriter, r *http.Request, actorID string, segments []string) {
	if len(segments) >= 4 && segments[3] == "unread-count" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeData(w, map[string]int{"count": a.unreadCount(actorID)})
		return
	}
	if len(segments) >= 4 {
		if r.Method != http.MethodPatch {
			methodNotAllowed(w, http.MethodPatch)
			return
		}
		writeData(w, map[string]any{})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writePage(w, a.notificationsFor(actorID), "")
	case http.MethodPatch:
		writeData(w, map[string]int{"count": 0})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (a *dummyAPI) handleFollowRequests(w http.ResponseWriter, r *http.Request, actorID string, segments []string) {
	if len(segments) >= 4 {
		if segments[3] == "sent" {
			if r.Method != http.MethodGet {
				methodNotAllowed(w, http.MethodGet)
				return
			}
			writePage(w, []connection{}, "")
			return
		}
		if r.Method != http.MethodPatch {
			methodNotAllowed(w, http.MethodPatch)
			return
		}
		writeData(w, map[string]any{})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	pending := connection{ID: "dev-conn-pending", Status: "pending", CreatedAt: "2026-09-01T12:44:00Z", Actor: a.data.connections["dev-actor-1"][0].Actor}
	writePage(w, []connection{pending}, "")
}

func (a *dummyAPI) handleAntennas(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) == 3 {
		switch r.Method {
		case http.MethodGet:
			writeData(w, a.data.antennas)
		case http.MethodPost:
			writeData(w, map[string]any{})
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}
	if len(segments) >= 5 && segments[4] == "notes" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writePage(w, a.timelineNotes(), "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeData(w, a.data.antennas[0])
	case http.MethodPatch, http.MethodDelete:
		writeData(w, map[string]any{})
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func (a *dummyAPI) handleMedia(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) >= 5 && segments[4] == "complete" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		writeData(w, map[string]string{"id": segments[3], "url": placeholderImage})
		return
	}
	if len(segments) >= 4 {
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodDelete)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	writeData(w, map[string]any{
		"id":             "dev-media-1",
		"url":            placeholderImage,
		"state":          "pending",
		"upload_url":     "/api/v1/dev-upload/dev-media-1",
		"upload_headers": map[string]string{},
		"expires_at":     "2026-12-31T00:00:00Z",
	})
}

func (a *dummyAPI) handleTimelines(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) != 2 || (segments[1] != "home" && segments[1] != "public") {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writePage(w, a.timelineNotes(), "")
}

func (a *dummyAPI) handleNotes(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) < 2 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	target, ok := a.data.notesByID[segments[1]]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "note not found")
		return
	}
	if len(segments) >= 3 && segments[2] == "thread" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		thread := []note{*target}
		for i := range a.data.notes {
			if a.data.notes[i].ReplyID == target.ID {
				thread = append(thread, a.data.notes[i])
			}
		}
		writePage(w, thread, "")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeData(w, target)
}

func (a *dummyAPI) handleEmojis(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) == 1 {
		switch r.Method {
		case http.MethodGet:
			writePage(w, a.data.emojis, "")
		case http.MethodPost:
			writeData(w, a.data.emojis[0])
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}
	if segments[1] == "import" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		writeData(w, a.data.emojis[0])
		return
	}
	switch r.Method {
	case http.MethodPatch:
		writeData(w, a.data.emojis[0])
	case http.MethodDelete:
		writeData(w, map[string]string{"id": segments[1]})
	default:
		methodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}

func (a *dummyAPI) handleProfiles(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) < 2 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	actorID := segments[1]
	if len(segments) == 2 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeData(w, a.data.profileFor(actorID))
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	switch segments[2] {
	case "notes":
		notes := make([]note, 0)
		for i := range a.data.notes {
			if a.data.notes[i].Author != nil && a.data.notes[i].Author.ID == actorID {
				notes = append(notes, a.data.notes[i])
			}
		}
		writePage(w, notes, "")
	case "followers", "following":
		writePage(w, a.connections(actorID), "")
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (a *dummyAPI) handleAccountSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPatch:
		writeData(w, a.data.accountSettings)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (a *dummyAPI) connections(actorID string) []connection {
	if list, ok := a.data.connections[actorID]; ok {
		return list
	}
	if list, ok := a.data.connections[demoSelectedActor]; ok {
		return list
	}
	return []connection{}
}

func (a *dummyAPI) notificationsFor(actorID string) []notification {
	result := make([]notification, 0, len(a.data.notifications))
	for _, item := range a.data.notifications {
		if item.ActorID == actorID {
			result = append(result, item)
		}
	}
	return result
}

func (a *dummyAPI) unreadCount(actorID string) int {
	count := 0
	for _, item := range a.notificationsFor(actorID) {
		if !item.IsRead {
			count++
		}
	}
	return count
}

func (a *dummyAPI) timelineNotes() []note {
	notes := make([]note, len(a.data.notes))
	copy(notes, a.data.notes)
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].CreatedAt > notes[j].CreatedAt })
	return notes
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"version": 1, "data": data})
}

func writePage(w http.ResponseWriter, data any, next string) {
	writeJSON(w, http.StatusOK, map[string]any{"version": 1, "data": data, "next": next})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

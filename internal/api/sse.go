package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/realtime"
)

const (
	sseHeartbeatInterval = 20 * time.Second
	sseReauthInterval    = 30 * time.Second
)

func (h *Handler) eventStream(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.events == nil {
		h.internalError(w, r, fmt.Errorf("realtime broker is not configured"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.internalError(w, r, fmt.Errorf("streaming is not supported by the response writer"))
		return
	}
	subscription, err := h.events.Subscribe(r.Context(), accountID)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("subscribe realtime account events: %w", err))
		return
	}
	defer subscription.Close()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()
	reauth := time.NewTicker(sseReauthInterval)
	defer reauth.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload, open := <-subscription.Channel():
			if !open {
				return
			}
			if err := writeSSEEvent(w, payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-reauth.C:
			currentAccountID, _, err := h.authenticate(r)
			if err != nil || currentAccountID != accountID {
				return
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, payload []byte) error {
	var event realtime.Event
	if err := json.Unmarshal(payload, &event); err != nil || event.Version != 1 || event.EventID == "" || event.Type == "" {
		return fmt.Errorf("invalid realtime event")
	}
	if strings.ContainsAny(event.EventID, "\r\n") || strings.ContainsAny(event.Type, "\r\n") {
		return fmt.Errorf("invalid realtime event fields")
	}
	if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.EventID, event.Type, payload); err != nil {
		return err
	}
	return nil
}

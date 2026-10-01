package api

import (
	"fmt"
	"net/http"
)

func (h *Handler) queueOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.queueStatus == nil {
		h.internalError(w, r, fmt.Errorf("queue status reader is not configured"))
		return
	}
	snapshot, err := h.queueStatus.Snapshot(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": snapshot})
}

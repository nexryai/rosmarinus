package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/antennas"
	"github.com/nexryai/rosmarinus/internal/readmodel"
)

type antennaView struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Source          antennas.Source `json:"source"`
	Users           []string        `json:"users"`
	Keywords        [][]string      `json:"keywords"`
	ExcludeKeywords [][]string      `json:"exclude_keywords"`
	CaseSensitive   bool            `json:"case_sensitive"`
	LocalOnly       bool            `json:"local_only"`
	ExcludeBots     bool            `json:"exclude_bots"`
	WithReplies     bool            `json:"with_replies"`
	WithFile        bool            `json:"with_file"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

func (h *Handler) antennas(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if _, ok := h.authorizeActor(w, r, accountID, actorID, false); !ok {
		return
	}
	if len(segments) == 0 {
		switch r.Method {
		case http.MethodGet:
			reader, ok := h.antennaReader(w, r)
			if !ok {
				return
			}
			items, err := reader.ListAntennas(r.Context(), accountID, actorID)
			if err != nil {
				h.internalError(w, r, fmt.Errorf("list antennas: %w", err))
				return
			}
			views := make([]antennaView, 0, len(items))
			for _, item := range items {
				views = append(views, projectAntenna(item))
			}
			h.writeJSON(w, http.StatusOK, map[string]any{"data": views})
		case http.MethodPost:
			var input connector.AntennaInput
			if h.decodeJSON(w, r, &input, false) {
				h.execute(w, r, accountID, connector.CommandAntennaCreate, actorID, input, http.StatusCreated)
			}
		default:
			h.methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return
	}
	antennaID := segments[0]
	if len(segments) == 2 && segments[1] == "notes" {
		if r.Method != http.MethodGet {
			h.methodNotAllowed(w, http.MethodGet)
			return
		}
		limit, after, ok := h.readPage(w, r)
		if !ok {
			return
		}
		reader, ok := h.antennaReader(w, r)
		if !ok {
			return
		}
		if antenna, err := reader.FindAntenna(r.Context(), accountID, actorID, antennaID); err != nil {
			h.internalError(w, r, fmt.Errorf("find antenna: %w", err))
			return
		} else if antenna == nil {
			h.writeError(w, http.StatusNotFound, "antenna_not_found", "Antenna not found")
			return
		}
		items, err := reader.ListAntennaNotes(r.Context(), accountID, actorID, antennaID, after, limit)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("list antenna notes: %w", err))
			return
		}
		h.writeNotePage(w, items, limit)
		return
	}
	if len(segments) != 1 {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		reader, ok := h.antennaReader(w, r)
		if !ok {
			return
		}
		item, err := reader.FindAntenna(r.Context(), accountID, actorID, antennaID)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("find antenna: %w", err))
			return
		}
		if item == nil {
			h.writeError(w, http.StatusNotFound, "antenna_not_found", "Antenna not found")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"data": projectAntenna(*item)})
	case http.MethodPatch:
		var input connector.AntennaInput
		if h.decodeJSON(w, r, &input, false) {
			h.execute(w, r, accountID, connector.CommandAntennaUpdate, actorID, connector.AntennaUpdateData{AntennaID: antennaID, AntennaInput: input}, http.StatusOK)
		}
	case http.MethodDelete:
		h.execute(w, r, accountID, connector.CommandAntennaDelete, actorID, connector.AntennaDeleteData{AntennaID: antennaID}, http.StatusOK)
	default:
		h.methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}

func projectAntenna(antenna antennas.Antenna) antennaView {
	return antennaView{
		ID: antenna.ID, Name: antenna.Name, Source: antenna.Source,
		Users: antenna.Users, Keywords: antenna.Keywords, ExcludeKeywords: antenna.ExcludeKeywords,
		CaseSensitive: antenna.CaseSensitive, LocalOnly: antenna.LocalOnly,
		ExcludeBots: antenna.ExcludeBots, WithReplies: antenna.WithReplies, WithFile: antenna.WithFile,
		CreatedAt: antenna.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: antenna.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (h *Handler) antennaReader(w http.ResponseWriter, r *http.Request) (readmodel.AntennaReader, bool) {
	reader, ok := h.reader.(readmodel.AntennaReader)
	if !ok {
		h.internalError(w, r, fmt.Errorf("antenna read service is not configured"))
	}
	return reader, ok
}

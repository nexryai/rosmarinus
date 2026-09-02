package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

const (
	MaxRequestBodyBytes = 1 << 20
	DefaultPageSize     = 20
	MaxPageSize         = 100
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Authenticator interface {
	Authenticate(*http.Request) (accountID, csrfToken string, err error)
}

type ActorStore interface {
	connector.ActorOwnershipLookup
	ListOwnedLocalActorsPage(context.Context, string, string, int, bool) ([]actors.Actor, error)
}

type Handler struct {
	authenticator Authenticator
	actors        ActorStore
	executor      connector.CommandExecutor
	receipts      connector.CommandReceiptStore
	authRoutes    http.Handler
	logger        *log.Logger
	now           func() time.Time
	receiptTTL    time.Duration
}

func NewHandler(authenticator Authenticator, actorStore ActorStore, executor connector.CommandExecutor, receipts connector.CommandReceiptStore, logger *log.Logger, receiptTTL time.Duration) http.Handler {
	return NewHandlerWithAuth(authenticator, actorStore, executor, receipts, nil, logger, receiptTTL)
}

func NewHandlerWithAuth(authenticator Authenticator, actorStore ActorStore, executor connector.CommandExecutor, receipts connector.CommandReceiptStore, authRoutes http.Handler, logger *log.Logger, receiptTTL time.Duration) http.Handler {
	if receiptTTL <= 0 {
		receiptTTL = 7 * 24 * time.Hour
	}
	return &Handler{
		authenticator: authenticator,
		actors:        actorStore,
		executor:      executor,
		receipts:      receipts,
		authRoutes:    authRoutes,
		logger:        logger,
		now:           func() time.Time { return time.Now().UTC() },
		receiptTTL:    receiptTTL,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && h.authRoutes != nil {
		h.authRoutes.ServeHTTP(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	accountID, csrfToken, err := h.authenticate(r)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if isMutation(r.Method) && !validCSRF(r.Header.Get("X-CSRF-Token"), csrfToken) {
		h.writeError(w, http.StatusForbidden, "csrf_failed", "CSRF token is missing or invalid")
		return
	}

	segments, err := pathSegments(strings.TrimPrefix(r.URL.Path, "/api/v1/"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_path", "path contains an invalid escape sequence")
		return
	}
	if len(segments) == 1 && segments[0] == "session" && r.Method == http.MethodGet {
		h.writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"account_id": accountID, "csrf_token": csrfToken}})
		return
	}
	if len(segments) == 1 && segments[0] == "actors" {
		h.actorsCollection(w, r, accountID)
		return
	}
	if len(segments) >= 2 && segments[0] == "actors" {
		h.actorResource(w, r, accountID, segments[1:])
		return
	}
	h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
}

func (h *Handler) authenticate(r *http.Request) (string, string, error) {
	if h == nil || h.authenticator == nil {
		return "", "", ErrUnauthenticated
	}
	accountID, csrfToken, err := h.authenticator.Authenticate(r)
	if err != nil || strings.TrimSpace(accountID) == "" {
		return "", "", ErrUnauthenticated
	}
	return accountID, csrfToken, nil
}

func (h *Handler) actorsCollection(w http.ResponseWriter, r *http.Request, accountID string) {
	switch r.Method {
	case http.MethodGet:
		if h.actors == nil {
			h.internalError(w, r, fmt.Errorf("actor store is not configured"))
			return
		}
		limit, err := pageLimit(r.URL.Query().Get("limit"))
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
			return
		}
		items, err := h.actors.ListOwnedLocalActorsPage(r.Context(), accountID, r.URL.Query().Get("after"), limit, false)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("list owned actors: %w", err))
			return
		}
		views := make([]actorView, 0, len(items))
		for i := range items {
			views = append(views, projectActor(&items[i]))
		}
		var next string
		if len(items) == limit {
			next = items[len(items)-1].ID
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"data": views, "next": next})
	case http.MethodPost:
		var data connector.ActorCreateData
		if !h.decodeJSON(w, r, &data, false) {
			return
		}
		h.execute(w, r, accountID, connector.CommandActorCreate, "", data, http.StatusCreated)
	default:
		h.methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (h *Handler) actorResource(w http.ResponseWriter, r *http.Request, accountID string, segments []string) {
	actorID := segments[0]
	if len(segments) == 1 {
		switch r.Method {
		case http.MethodGet:
			actor, ok := h.authorizeActor(w, r, accountID, actorID, false)
			if ok {
				h.writeJSON(w, http.StatusOK, map[string]any{"data": projectActor(actor)})
			}
		case http.MethodPatch:
			var data connector.ActorUpdateData
			if h.decodeJSON(w, r, &data, false) {
				h.execute(w, r, accountID, connector.CommandActorUpdate, actorID, data, http.StatusOK)
			}
		case http.MethodDelete:
			h.execute(w, r, accountID, connector.CommandActorDelete, actorID, map[string]any{}, http.StatusOK)
		default:
			h.methodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
		return
	}

	switch segments[1] {
	case "posts":
		h.posts(w, r, accountID, actorID, segments[2:])
	case "poll-votes":
		if len(segments) == 2 && r.Method == http.MethodPost {
			var data connector.PollVoteData
			if h.decodeJSON(w, r, &data, false) {
				h.execute(w, r, accountID, connector.CommandPollVote, actorID, data, http.StatusCreated)
			}
			return
		}
		h.methodOrNotFound(w, r, http.MethodPost, len(segments) == 2)
	case "reactions":
		h.reactions(w, r, accountID, actorID, segments[2:])
	case "follows":
		h.targetMutation(w, r, accountID, actorID, segments[2:], connector.CommandFollowCreate, connector.CommandFollowDelete)
	case "blocks":
		h.targetMutation(w, r, accountID, actorID, segments[2:], connector.CommandBlockCreate, connector.CommandBlockDelete)
	case "follow-requests":
		h.followRequest(w, r, accountID, actorID, segments[2:])
	case "notifications":
		h.notification(w, r, accountID, actorID, segments[2:])
	default:
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (h *Handler) posts(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if len(segments) == 0 && r.Method == http.MethodPost {
		var data connector.PostCreateData
		if h.decodeJSON(w, r, &data, false) {
			h.execute(w, r, accountID, connector.CommandPostCreate, actorID, data, http.StatusCreated)
		}
		return
	}
	if len(segments) == 1 && r.Method == http.MethodDelete {
		h.execute(w, r, accountID, connector.CommandPostDelete, actorID, connector.PostDeleteData{NoteID: segments[0]}, http.StatusOK)
		return
	}
	h.methodOrNotFound(w, r, http.MethodPost, len(segments) == 0)
}

func (h *Handler) reactions(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if len(segments) != 1 {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	data := connector.ReactionCreateData{NoteID: segments[0]}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Reaction string `json:"reaction"`
		}
		if h.decodeJSON(w, r, &body, false) {
			data.Reaction = body.Reaction
			h.execute(w, r, accountID, connector.CommandReactionCreate, actorID, data, http.StatusCreated)
		}
	case http.MethodDelete:
		h.execute(w, r, accountID, connector.CommandReactionDelete, actorID, connector.ReactionDeleteData{NoteID: segments[0]}, http.StatusOK)
	default:
		h.methodNotAllowed(w, http.MethodPut, http.MethodDelete)
	}
}

func (h *Handler) targetMutation(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string, createCommand, deleteCommand string) {
	if len(segments) != 0 {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	var data struct {
		Target string `json:"target"`
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		h.methodNotAllowed(w, http.MethodPost, http.MethodDelete)
		return
	}
	if !h.decodeJSON(w, r, &data, false) {
		return
	}
	command := createCommand
	status := http.StatusCreated
	if r.Method == http.MethodDelete {
		command = deleteCommand
		status = http.StatusOK
	}
	h.execute(w, r, accountID, command, actorID, data, status)
}

func (h *Handler) followRequest(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if len(segments) != 1 || r.Method != http.MethodPatch {
		h.methodOrNotFound(w, r, http.MethodPatch, len(segments) == 1)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !h.decodeJSON(w, r, &body, false) {
		return
	}
	data := connector.FollowApproveData{FollowerID: segments[0]}
	switch body.Status {
	case "accepted":
		h.execute(w, r, accountID, connector.CommandFollowApprove, actorID, data, http.StatusOK)
	case "rejected":
		h.execute(w, r, accountID, connector.CommandFollowReject, actorID, connector.FollowRejectData{FollowerID: segments[0]}, http.StatusOK)
	default:
		h.writeError(w, http.StatusUnprocessableEntity, "invalid_status", "status must be accepted or rejected")
	}
}

func (h *Handler) notification(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if len(segments) != 1 || r.Method != http.MethodPatch {
		h.methodOrNotFound(w, r, http.MethodPatch, len(segments) == 1)
		return
	}
	var body struct {
		IsRead bool `json:"is_read"`
	}
	if !h.decodeJSON(w, r, &body, false) {
		return
	}
	if !body.IsRead {
		h.writeError(w, http.StatusUnprocessableEntity, "invalid_read_state", "is_read must be true")
		return
	}
	h.execute(w, r, accountID, connector.CommandNotificationMarkRead, actorID, connector.NotificationMarkReadData{NotificationID: segments[0]}, http.StatusOK)
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request, accountID, command, actorID string, data any, successStatus int) {
	requestID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(requestID) < 16 || len(requestID) > 200 {
		h.writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must contain 16 to 200 characters")
		return
	}
	if command != connector.CommandActorCreate {
		includeDeleted := command == connector.CommandActorDelete
		actor, ok := h.authorizeActor(w, r, accountID, actorID, includeDeleted)
		if !ok {
			return
		}
		actorID = actor.ID
	}

	receipt := connector.CommandReceipt{
		AccountID: accountID,
		RequestID: requestID,
		Command:   command,
		ActorID:   actorID,
		Status:    connector.CommandReceiptPending,
		CreatedAt: h.now(),
		UpdatedAt: h.now(),
		ExpiresAt: h.now().Add(h.receiptTTL),
	}
	if h.receipts != nil {
		existing, claimed, err := h.receipts.Claim(r.Context(), receipt)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("claim REST command receipt: %w", err))
			return
		}
		if !claimed {
			h.writeReceipt(w, existing, command, actorID, successStatus)
			return
		}
	}

	result, resultActorID, err := connector.ExecuteCommand(r.Context(), h.executor, command, accountID, actorID, data)
	if err != nil {
		if h.receipts != nil {
			if receiptErr := h.receipts.Fail(r.Context(), accountID, requestID, "operation_failed", h.now()); receiptErr != nil {
				h.internalError(w, r, fmt.Errorf("record REST failure after %v: %w", err, receiptErr))
				return
			}
		}
		if h.logger != nil {
			h.logger.Printf("api: operation failed account_id=%s actor_id=%s command=%s request_id=%s err=%v", accountID, actorID, command, requestID, err)
		}
		h.writeError(w, http.StatusUnprocessableEntity, "operation_failed", "operation could not be completed")
		return
	}
	if h.receipts != nil {
		if err := h.receipts.Complete(r.Context(), accountID, requestID, resultActorID, result, h.now()); err != nil {
			h.internalError(w, r, fmt.Errorf("complete REST command receipt: %w", err))
			return
		}
	}
	h.writeJSON(w, successStatus, map[string]any{"data": result})
}

func (h *Handler) writeReceipt(w http.ResponseWriter, receipt *connector.CommandReceipt, command, actorID string, successStatus int) {
	if receipt == nil || receipt.Command != command || (command != connector.CommandActorCreate && receipt.ActorID != actorID) {
		h.writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for another operation")
		return
	}
	switch receipt.Status {
	case connector.CommandReceiptCompleted:
		h.writeJSON(w, successStatus, map[string]any{"data": receipt.Result, "replayed": true})
	case connector.CommandReceiptPending:
		h.writeError(w, http.StatusConflict, "operation_in_progress", "operation is still in progress")
	case connector.CommandReceiptFailed:
		code := receipt.ErrorCode
		if code == "" {
			code = "operation_failed"
		}
		h.writeError(w, http.StatusUnprocessableEntity, code, "the previous operation failed")
	default:
		h.writeError(w, http.StatusConflict, "idempotency_conflict", "stored operation has an invalid state")
	}
}

func (h *Handler) authorizeActor(w http.ResponseWriter, r *http.Request, accountID, actorID string, includeDeleted bool) (*actors.Actor, bool) {
	if h.actors == nil {
		h.internalError(w, r, fmt.Errorf("actor store is not configured"))
		return nil, false
	}
	var actor *actors.Actor
	var err error
	if includeDeleted {
		actor, err = h.actors.FindOwnedLocalByIDIncludingDeleted(r.Context(), accountID, actorID)
	} else {
		actor, err = h.actors.FindOwnedLocalByID(r.Context(), accountID, actorID)
	}
	if err != nil {
		h.internalError(w, r, fmt.Errorf("authorize actor: %w", err))
		return nil, false
	}
	if actor == nil {
		h.writeError(w, http.StatusNotFound, "actor_not_found", "Actor not found")
		return nil, false
	}
	return actor, true
}

func (h *Handler) decodeJSON(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	reader := http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return true
		}
		h.writeError(w, http.StatusBadRequest, "invalid_json", "request body must be one valid JSON object")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain one JSON object")
		return false
	}
	return true
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	if h.logger != nil {
		h.logger.Printf("api: request failed method=%s path=%s err=%v", r.Method, r.URL.Path, err)
	}
	h.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func (h *Handler) methodOrNotFound(w http.ResponseWriter, r *http.Request, allowed string, pathMatches bool) {
	if pathMatches {
		h.methodNotAllowed(w, allowed)
		return
	}
	h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
}

func (h *Handler) methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func validCSRF(actual, expected string) bool {
	if actual == "" || expected == "" || len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func isMutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func pageLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultPageSize, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxPageSize {
		return 0, fmt.Errorf("limit must be between 1 and %d", MaxPageSize)
	}
	return limit, nil
}

func pathSegments(path string) ([]string, error) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil, nil
	}
	raw := strings.Split(trimmed, "/")
	segments := make([]string, 0, len(raw))
	for _, item := range raw {
		decoded, err := url.PathUnescape(item)
		if err != nil || decoded == "" || strings.Contains(decoded, "/") {
			return nil, fmt.Errorf("invalid path segment")
		}
		segments = append(segments, decoded)
	}
	return segments, nil
}

type actorView struct {
	ID             string                `json:"id"`
	Username       string                `json:"username"`
	Name           string                `json:"name"`
	Summary        string                `json:"summary"`
	URL            string                `json:"url"`
	ProfileFields  []actors.ProfileField `json:"profile_fields"`
	Birthday       string                `json:"birthday"`
	Location       string                `json:"location"`
	AvatarURL      string                `json:"avatar_url"`
	BannerURL      string                `json:"banner_url"`
	Tags           []string              `json:"tags"`
	EmojiNames     []string              `json:"emoji_names"`
	IsBot          bool                  `json:"is_bot"`
	IsCat          bool                  `json:"is_cat"`
	IsLocked       bool                  `json:"is_locked"`
	IsDiscoverable bool                  `json:"is_discoverable"`
	Type           string                `json:"type"`
	URI            string                `json:"uri"`
	IsSuspended    bool                  `json:"is_suspended"`
}

func projectActor(actor *actors.Actor) actorView {
	return actorView{
		ID: actor.ID, Username: actor.Username, Name: actor.Name, Summary: actor.Summary,
		URL: actor.URL, ProfileFields: actor.ProfileFields, Birthday: actor.Birthday,
		Location: actor.Location, AvatarURL: actor.AvatarURL, BannerURL: actor.BannerURL,
		Tags: actor.Tags, EmojiNames: actor.EmojiNames, IsBot: actor.IsBot,
		IsCat: actor.IsCat, IsLocked: actor.IsLocked, IsDiscoverable: actor.IsDiscoverable,
		Type: actor.Type, URI: actor.URI, IsSuspended: actor.IsSuspended,
	}
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type fakeAuthenticator struct {
	session *Session
	err     error
}

func (a fakeAuthenticator) Authenticate(*http.Request) (string, string, error) {
	if a.session == nil {
		return "", "", a.err
	}
	return a.session.AccountID, a.session.CSRFToken, a.err
}

type Session struct {
	AccountID string
	CSRFToken string
}

type fakeActorStore struct {
	actors []actors.Actor
	err    error
}

func (s *fakeActorStore) FindOwnedLocalByID(_ context.Context, accountID, actorID string) (*actors.Actor, error) {
	return s.find(accountID, actorID, false)
}

func (s *fakeActorStore) FindOwnedLocalByIDIncludingDeleted(_ context.Context, accountID, actorID string) (*actors.Actor, error) {
	return s.find(accountID, actorID, true)
}

func (s *fakeActorStore) find(accountID, actorID string, includeDeleted bool) (*actors.Actor, error) {
	if s.err != nil {
		return nil, s.err
	}
	for i := range s.actors {
		actor := &s.actors[i]
		if actor.ID == actorID && actor.OwnerAccountID == accountID && (includeDeleted || actor.DeletedAt == nil) {
			return actor, nil
		}
	}
	return nil, nil
}

func (s *fakeActorStore) ListOwnedLocalActorsPage(_ context.Context, accountID, afterID string, limit int, _ bool) ([]actors.Actor, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make([]actors.Actor, 0, limit)
	for _, actor := range s.actors {
		if actor.OwnerAccountID == accountID && actor.ID > afterID && actor.DeletedAt == nil {
			result = append(result, actor)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

type fakeExecutor struct {
	command string
	actorID string
	data    any
	err     error
}

func (e *fakeExecutor) record(command, actorID string, data any) error {
	e.command, e.actorID, e.data = command, actorID, data
	return e.err
}

func (e *fakeExecutor) CreateFollow(_ context.Context, actorID, target string) (string, error) {
	err := e.record(connector.CommandFollowCreate, actorID, target)
	return "follow-1", err
}

func (e *fakeExecutor) DeleteFollow(_ context.Context, command connector.FollowDeleteCommand) (connector.FollowDeleted, error) {
	err := e.record(connector.CommandFollowDelete, command.ActorID, command)
	return connector.FollowDeleted{FollowerID: command.ActorID, FolloweeID: "remote-1"}, err
}

func (e *fakeExecutor) ApproveFollow(_ context.Context, followerID, followeeID string) (string, error) {
	err := e.record(connector.CommandFollowApprove, followeeID, followerID)
	return "follow-1", err
}

func (e *fakeExecutor) RejectFollow(_ context.Context, followerID, followeeID string) (string, error) {
	err := e.record(connector.CommandFollowReject, followeeID, followerID)
	return "follow-1", err
}

func (e *fakeExecutor) CreatePost(_ context.Context, command connector.PostCreateCommand) (connector.PostCreated, error) {
	err := e.record(connector.CommandPostCreate, command.ActorID, command)
	return connector.PostCreated{NoteID: command.NoteID}, err
}

func (e *fakeExecutor) DeletePost(_ context.Context, command connector.PostDeleteCommand) (connector.PostDeleted, error) {
	err := e.record(connector.CommandPostDelete, command.ActorID, command)
	return connector.PostDeleted{ActorID: command.ActorID, NoteID: command.NoteID}, err
}

func (e *fakeExecutor) VotePoll(_ context.Context, command connector.PollVoteCommand) (connector.PollVoted, error) {
	err := e.record(connector.CommandPollVote, command.ActorID, command)
	return connector.PollVoted{NoteID: command.NoteID, Choice: command.Choice}, err
}

func (e *fakeExecutor) CreateReaction(_ context.Context, command connector.ReactionCreateCommand) (connector.ReactionCreated, error) {
	err := e.record(connector.CommandReactionCreate, command.ActorID, command)
	return connector.ReactionCreated{NoteID: command.NoteID, Reaction: command.Reaction}, err
}

func (e *fakeExecutor) DeleteReaction(_ context.Context, command connector.ReactionDeleteCommand) (connector.ReactionDeleted, error) {
	err := e.record(connector.CommandReactionDelete, command.ActorID, command)
	return connector.ReactionDeleted{NoteID: command.NoteID}, err
}

func (e *fakeExecutor) CreateBlock(_ context.Context, command connector.BlockCreateCommand) (connector.BlockCreated, error) {
	err := e.record(connector.CommandBlockCreate, command.ActorID, command)
	return connector.BlockCreated{BlockID: "block-1"}, err
}

func (e *fakeExecutor) DeleteBlock(_ context.Context, command connector.BlockDeleteCommand) (connector.BlockDeleted, error) {
	err := e.record(connector.CommandBlockDelete, command.ActorID, command)
	return connector.BlockDeleted{BlockID: "block-1"}, err
}

func (e *fakeExecutor) CreateActor(_ context.Context, accountID string, command connector.ActorCreateCommand) (connector.ActorCreated, error) {
	err := e.record(connector.CommandActorCreate, accountID, command)
	return connector.ActorCreated{ActorID: "actor-created", Username: command.Username}, err
}

func (e *fakeExecutor) UpdateActor(_ context.Context, accountID string, command connector.ActorUpdateCommand) (connector.ActorUpdated, error) {
	err := e.record(connector.CommandActorUpdate, command.ActorID, command)
	return connector.ActorUpdated{ActorID: command.ActorID}, err
}

func (e *fakeExecutor) DeleteActor(_ context.Context, accountID string, command connector.ActorDeleteCommand) (connector.ActorDeleted, error) {
	err := e.record(connector.CommandActorDelete, command.ActorID, command)
	return connector.ActorDeleted{ActorID: command.ActorID}, err
}

func (e *fakeExecutor) MarkNotificationRead(_ context.Context, accountID, actorID, notificationID string) (connector.NotificationRead, error) {
	err := e.record(connector.CommandNotificationMarkRead, actorID, notificationID)
	return connector.NotificationRead{NotificationID: notificationID, IsRead: true}, err
}

type fakeReceiptStore struct {
	receipts map[string]connector.CommandReceipt
}

func (s *fakeReceiptStore) Claim(_ context.Context, receipt connector.CommandReceipt) (*connector.CommandReceipt, bool, error) {
	if s.receipts == nil {
		s.receipts = make(map[string]connector.CommandReceipt)
	}
	key := receipt.AccountID + ":" + receipt.RequestID
	if existing, ok := s.receipts[key]; ok {
		copy := existing
		return &copy, false, nil
	}
	s.receipts[key] = receipt
	copy := receipt
	return &copy, true, nil
}

func (s *fakeReceiptStore) Complete(_ context.Context, accountID, requestID, actorID string, result any, now time.Time) error {
	key := accountID + ":" + requestID
	receipt := s.receipts[key]
	receipt.Status, receipt.ActorID, receipt.Result, receipt.UpdatedAt = connector.CommandReceiptCompleted, actorID, result, now
	s.receipts[key] = receipt
	return nil
}

func (s *fakeReceiptStore) Fail(_ context.Context, accountID, requestID, code string, now time.Time) error {
	key := accountID + ":" + requestID
	receipt := s.receipts[key]
	receipt.Status, receipt.ErrorCode, receipt.UpdatedAt = connector.CommandReceiptFailed, code, now
	s.receipts[key] = receipt
	return nil
}

func TestHandlerRequiresAuthentication(t *testing.T) {
	handler := NewHandler(fakeAuthenticator{err: ErrUnauthenticated}, &fakeActorStore{}, &fakeExecutor{}, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/actors", nil))
	assertError(t, recorder, http.StatusUnauthorized, "unauthenticated")
}

func TestHandlerRequiresCSRFForMutation(t *testing.T) {
	handler, _, _ := testHandler()
	recorder := httptest.NewRecorder()
	req := jsonRequest(http.MethodPost, "/api/v1/actors", `{"username":"alice"}`)
	req.Header.Del("X-CSRF-Token")
	handler.ServeHTTP(recorder, req)
	assertError(t, recorder, http.StatusForbidden, "csrf_failed")
}

func TestHandlerListsOnlyOwnedActorsWithoutPrivateKeys(t *testing.T) {
	handler, _, store := testHandler()
	store.actors = append(store.actors,
		actors.Actor{ID: "actor-2", OwnerAccountID: "account-2", Username: "mallory"},
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/actors?limit=1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("private")) || bytes.Contains(recorder.Body.Bytes(), []byte("owner")) {
		t.Fatalf("private server fields leaked: %s", recorder.Body.String())
	}
	var body struct {
		Data []actorView `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "actor-1" {
		t.Fatalf("actors = %+v", body.Data)
	}
}

func TestHandlerRejectsCrossAccountActor(t *testing.T) {
	handler, _, store := testHandler()
	store.actors = append(store.actors, actors.Actor{ID: "actor-2", OwnerAccountID: "account-2"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, jsonRequest(http.MethodPost, "/api/v1/actors/actor-2/posts", `{"note_id":"note-1","text":"hello"}`))
	assertError(t, recorder, http.StatusNotFound, "actor_not_found")
}

func TestHandlerMapsRESTPostToDomainCommand(t *testing.T) {
	handler, executor, _ := testHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, jsonRequest(http.MethodPost, "/api/v1/actors/actor-1/posts", `{"note_id":"note-1","text":"hello"}`))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	command, ok := executor.data.(connector.PostCreateCommand)
	if executor.command != connector.CommandPostCreate || executor.actorID != "actor-1" || !ok || command.Text != "hello" {
		t.Fatalf("execution = command:%q actor:%q data:%+v", executor.command, executor.actorID, executor.data)
	}
}

func TestHandlerReplaysIdempotentResultAndRejectsKeyReuse(t *testing.T) {
	handler, executor, _ := testHandler()
	first := jsonRequest(http.MethodPost, "/api/v1/actors/actor-1/posts", `{"note_id":"note-1","text":"hello"}`)
	first.Header.Set("Idempotency-Key", "same-request-key")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, first)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	executor.command = ""
	second := jsonRequest(http.MethodPost, "/api/v1/actors/actor-1/posts", `{"note_id":"note-1","text":"hello"}`)
	second.Header.Set("Idempotency-Key", "same-request-key")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, second)
	if recorder.Code != http.StatusCreated || executor.command != "" || !bytes.Contains(recorder.Body.Bytes(), []byte(`"replayed":true`)) {
		t.Fatalf("replay status=%d execution=%q body=%s", recorder.Code, executor.command, recorder.Body.String())
	}

	conflict := jsonRequest(http.MethodPost, "/api/v1/actors/actor-1/follows", `{"target":"alice@example.test"}`)
	conflict.Header.Set("Idempotency-Key", "same-request-key")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, conflict)
	assertError(t, recorder, http.StatusConflict, "idempotency_conflict")
}

func TestHandlerRejectsUnknownJSONFields(t *testing.T) {
	handler, _, _ := testHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, jsonRequest(http.MethodPost, "/api/v1/actors/actor-1/posts", `{"note_id":"note-1","text":"hello","account_id":"account-2"}`))
	assertError(t, recorder, http.StatusBadRequest, "invalid_json")
}

func TestHandlerMapsFollowApprovalAndNotificationRead(t *testing.T) {
	handler, executor, _ := testHandler()
	tests := []struct {
		method  string
		path    string
		body    string
		command string
	}{
		{http.MethodPatch, "/api/v1/actors/actor-1/follow-requests/remote-1", `{"status":"accepted"}`, connector.CommandFollowApprove},
		{http.MethodPatch, "/api/v1/actors/actor-1/notifications/notification-1", `{"is_read":true}`, connector.CommandNotificationMarkRead},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := jsonRequest(test.method, test.path, test.body)
			req.Header.Set("Idempotency-Key", "request-"+test.command+"-123456")
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK || executor.command != test.command {
				t.Fatalf("status=%d command=%q body=%s", recorder.Code, executor.command, recorder.Body.String())
			}
		})
	}
}

func TestHandlerDoesNotExposeInternalErrors(t *testing.T) {
	handler, executor, _ := testHandler()
	executor.err = errors.New("database password was logged")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, jsonRequest(http.MethodPost, "/api/v1/actors/actor-1/posts", `{"note_id":"note-1","text":"hello"}`))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", recorder.Code)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("database password")) {
		t.Fatalf("internal error leaked: %s", recorder.Body.String())
	}
}

func testHandler() (http.Handler, *fakeExecutor, *fakeActorStore) {
	executor := &fakeExecutor{}
	store := &fakeActorStore{actors: []actors.Actor{{
		ID: "actor-1", OwnerAccountID: "account-1", Username: "alice", Name: "Alice",
		URI: "https://example.test/users/actor-1", PrivateKeyPEM: "secret",
	}}}
	handler := NewHandler(
		fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf-token"}},
		store,
		executor,
		&fakeReceiptStore{},
		nil,
		time.Hour,
	)
	return handler, executor, store
}

func jsonRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "csrf-token")
	req.Header.Set("Idempotency-Key", "request-1234567890")
	return req
}

func assertError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d want %d body=%s", recorder.Code, status, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != code {
		t.Fatalf("error code = %q want %q", body.Error.Code, code)
	}
}

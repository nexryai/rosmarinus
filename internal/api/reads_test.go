package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/readmodel"
)

type fakeReader struct {
	publicItems   []readmodel.Note
	note          *readmodel.Note
	accountID     string
	actorID       string
	targetActorID string
	notifications []readmodel.Notification
	profile       *readmodel.Profile
	unread        *bool
	calls         int
}

func (f *fakeReader) ListPublicTimeline(_ context.Context, actorID string, _ readmodel.Cursor, _ int) ([]readmodel.Note, error) {
	f.actorID, f.calls = actorID, f.calls+1
	return f.publicItems, nil
}

func (f *fakeReader) ListHomeTimeline(_ context.Context, actorID string, _ readmodel.Cursor, _ int) ([]readmodel.Note, error) {
	f.actorID, f.calls = actorID, f.calls+1
	return f.publicItems, nil
}

func (f *fakeReader) FindVisibleNote(_ context.Context, actorID, _ string) (*readmodel.Note, error) {
	f.actorID, f.calls = actorID, f.calls+1
	return f.note, nil
}

func (f *fakeReader) ListVisibleThread(_ context.Context, actorID, _ string, _ readmodel.Cursor, _ int) ([]readmodel.Note, error) {
	f.actorID, f.calls = actorID, f.calls+1
	return f.publicItems, nil
}

func (f *fakeReader) ListConnections(_ context.Context, viewerActorID, targetActorID, _, _ string, _ int) ([]readmodel.Connection, error) {
	f.actorID, f.targetActorID, f.calls = viewerActorID, targetActorID, f.calls+1
	return []readmodel.Connection{{Follow: follows.Follow{ID: "follow-1"}, Actor: &actors.Actor{ID: "remote-1"}}}, nil
}

func (f *fakeReader) ListNotifications(_ context.Context, accountID, actorID string, _ readmodel.Cursor, _ int, unread *bool) ([]readmodel.Notification, error) {
	f.accountID, f.actorID, f.calls = accountID, actorID, f.calls+1
	f.unread = unread
	return f.notifications, nil
}

func (f *fakeReader) ListLocalEmojis(context.Context, string, int) ([]emojis.Emoji, error) {
	f.calls++
	return []emojis.Emoji{{Name: "salvia", PublicURL: "https://example.test/emoji.webp"}}, nil
}

func (f *fakeReader) FindProfile(_ context.Context, viewerActorID, actorID string) (*readmodel.Profile, error) {
	f.actorID, f.calls = viewerActorID, f.calls+1
	if f.profile != nil {
		return f.profile, nil
	}
	return &readmodel.Profile{Actor: &actors.Actor{ID: actorID}}, nil
}

func TestReadTimelineRequiresOwnedActor(t *testing.T) {
	reader := &fakeReader{}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-2", OwnerAccountID: "account-2"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/timelines/public?actor_id=actor-2", nil))
	assertError(t, recorder, http.StatusNotFound, "actor_not_found")
	if reader.calls != 0 {
		t.Fatalf("reader called %d times after failed ownership check", reader.calls)
	}
}

func TestReadTimelineReturnsSafeProjectionAndCursor(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	reader := &fakeReader{publicItems: []readmodel.Note{{
		Note: notes.Note{
			ID: "note-1", URI: "https://remote.test/notes/1", AuthorID: "remote-1", Text: "hello",
			Visibility: notes.VisibilitySpecified, VisibleUserURIs: []string{"https://example.test/users/private"},
			Raw: map[string]any{"private": "federation document"}, CreatedAt: now,
		},
		Author: &actors.Actor{ID: "remote-1", Username: "remote", PrivateKeyPEM: "secret"},
	}}}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/timelines/public?actor_id=actor-1&limit=1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["next"] == "" {
		t.Fatal("expected a next cursor")
	}
	encoded := recorder.Body.String()
	for _, forbidden := range []string{"visible_user", "federation document", "PrivateKey", "secret"} {
		if contains := stringContains(encoded, forbidden); contains {
			t.Fatalf("response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestReadNotificationsPassesAuthenticatedScope(t *testing.T) {
	reader := &fakeReader{}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/actors/actor-1/notifications?unread=true", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if reader.accountID != "account-1" || reader.actorID != "actor-1" {
		t.Fatalf("scope = account %q actor %q", reader.accountID, reader.actorID)
	}
}

func TestReadNotificationsPassesReadFilter(t *testing.T) {
	reader := &fakeReader{}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/actors/actor-1/notifications?unread=false", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if reader.unread == nil || *reader.unread {
		t.Fatalf("unread filter = %v, want false", reader.unread)
	}
}

func TestAccountNotificationsOmitActorDependentProjections(t *testing.T) {
	reader := &fakeReader{notifications: []readmodel.Notification{{
		Notification: notifications.Notification{ID: "notification-1", RecipientAccountID: "account-1", SourceActorID: "remote-1", NoteID: "note-1"},
		Source:       &actors.Actor{ID: "remote-1", Username: "remote"},
		Note:         &readmodel.Note{Note: notes.Note{ID: "note-1", Text: "private text"}},
	}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, &fakeActorStore{}, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("private text")) || bytes.Contains(recorder.Body.Bytes(), []byte("remote")) {
		t.Fatalf("account notification leaked Actor-dependent projection: %s", recorder.Body.String())
	}
}

func TestReadRejectsMalformedCursor(t *testing.T) {
	reader := &fakeReader{}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/timelines/home?actor_id=actor-1&after=not-base64!", nil))
	assertError(t, recorder, http.StatusBadRequest, "invalid_cursor")
}

func TestProfileConnectionsUseOwnedViewerForBlockFiltering(t *testing.T) {
	reader := &fakeReader{}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/profiles/remote-profile/followers?actor_id=actor-1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if reader.actorID != "actor-1" || reader.targetActorID != "remote-profile" {
		t.Fatalf("connection scope = viewer %q target %q", reader.actorID, reader.targetActorID)
	}
}

func TestProfileReturnsViewerRelationshipState(t *testing.T) {
	reader := &fakeReader{profile: &readmodel.Profile{Actor: &actors.Actor{ID: "remote-profile"}, FollowStatus: "pending", BlockedByViewer: true}}
	store := &fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}}
	handler := NewHandlerWithAuthAndReader(fakeAuthenticator{session: &Session{AccountID: "account-1"}}, store, &fakeExecutor{}, nil, reader, nil, nil, 0)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/profiles/remote-profile?actor_id=actor-1", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"follow_status":"pending"`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"blocked_by_viewer":true`)) {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func stringContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
)

type fakeEmojiAdmin struct {
	action   string
	actorID  string
	id       string
	name     string
	mediaID  string
	sourceID string
	err      error
}

func (a *fakeEmojiAdmin) CreateFromMedia(_ context.Context, actorID, name, mediaID string) (*emojis.Emoji, error) {
	a.action, a.actorID, a.name, a.mediaID = "create", actorID, name, mediaID
	return &emojis.Emoji{ID: "emoji-1", Name: name, PublicURL: "https://local.test/media/1"}, a.err
}

func (a *fakeEmojiAdmin) Update(_ context.Context, actorID, id, name, mediaID string) (*emojis.Emoji, error) {
	a.action, a.actorID, a.id, a.name, a.mediaID = "update", actorID, id, name, mediaID
	return &emojis.Emoji{ID: id, Name: name, PublicURL: "https://local.test/media/1"}, a.err
}

func (a *fakeEmojiAdmin) Delete(_ context.Context, id string) error {
	a.action, a.id = "delete", id
	return a.err
}

func (a *fakeEmojiAdmin) ImportRemote(_ context.Context, actorID, sourceID, name string) (*emojis.Emoji, error) {
	a.action, a.actorID, a.sourceID, a.name = "import", actorID, sourceID, name
	return &emojis.Emoji{ID: "emoji-imported", Name: name, PublicURL: "https://local.test/media/imported"}, a.err
}

func newEmojiAdminHandler(admin EmojiAdmin, reader *fakeReader) http.Handler {
	return NewHandlerCompleteWithEmojiAdmin(
		fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf-1"}},
		&fakeActorStore{actors: []actors.Actor{{ID: "actor-1", OwnerAccountID: "account-1"}}},
		&fakeExecutor{}, nil, reader, nil, InstanceInfo{}, nil, nil, nil, nil, admin, 0, nil, nil, 0,
	)
}

func TestEmojiListSupportsObservedRemoteFilters(t *testing.T) {
	reader := &fakeReader{}
	handler := newEmojiAdminHandler(&fakeEmojiAdmin{}, reader)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/emojis?scope=remote&query=party&host=remote.test&limit=30", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !reader.emojiQuery.Remote || reader.emojiQuery.Query != "party" || reader.emojiQuery.Host != "remote.test" || reader.emojiQuery.Limit != 30 {
		t.Fatalf("unexpected emoji query: %+v", reader.emojiQuery)
	}
}

func TestEmojiCreateRequiresOwnedActorAndDelegatesMedia(t *testing.T) {
	admin := &fakeEmojiAdmin{}
	handler := newEmojiAdminHandler(admin, &fakeReader{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/emojis", strings.NewReader(`{"actor_id":"actor-1","name":"party","media_id":"media-1"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || admin.action != "create" || admin.actorID != "actor-1" || admin.name != "party" || admin.mediaID != "media-1" {
		t.Fatalf("unexpected create: status=%d admin=%+v body=%s", recorder.Code, admin, recorder.Body.String())
	}
}

func TestEmojiImportMapsDuplicateName(t *testing.T) {
	admin := &fakeEmojiAdmin{err: emojis.ErrNameConflict}
	handler := newEmojiAdminHandler(admin, &fakeReader{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/emojis/import", strings.NewReader(`{"actor_id":"actor-1","source_id":"remote-1","name":"party"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	handler.ServeHTTP(recorder, request)

	assertError(t, recorder, http.StatusConflict, "emoji_name_conflict")
	if admin.action != "import" || admin.sourceID != "remote-1" || !errors.Is(admin.err, emojis.ErrNameConflict) {
		t.Fatalf("unexpected import call: %+v", admin)
	}
}

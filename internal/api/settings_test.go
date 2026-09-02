package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/settings"
)

type fakeSettingsStore struct {
	accountID string
	actorID   string
	account   settings.Account
	actor     settings.Actor
}

func (s *fakeSettingsStore) GetAccount(_ context.Context, accountID string) (*settings.Account, error) {
	s.accountID = accountID
	return &s.account, nil
}

func (s *fakeSettingsStore) UpdateAccount(_ context.Context, accountID string, patch settings.AccountPatch) (*settings.Account, error) {
	s.accountID = accountID
	if patch.Theme != nil {
		s.account.Theme = *patch.Theme
	}
	return &s.account, nil
}

func (s *fakeSettingsStore) GetActor(_ context.Context, accountID, actorID string) (*settings.Actor, error) {
	s.accountID, s.actorID = accountID, actorID
	return &s.actor, nil
}

func (s *fakeSettingsStore) UpdateActor(_ context.Context, accountID, actorID string, patch settings.ActorPatch) (*settings.Actor, error) {
	s.accountID, s.actorID = accountID, actorID
	s.actor.AccountID, s.actor.ActorID = accountID, actorID
	if patch.DefaultVisibility != nil {
		s.actor.DefaultVisibility = *patch.DefaultVisibility
	}
	return &s.actor, nil
}

func TestAccountSettingsUpdateUsesSessionAccount(t *testing.T) {
	settingsStore := &fakeSettingsStore{account: settings.Account{Theme: settings.DefaultTheme}}
	handler := NewHandlerWithServices(
		fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf-token"}},
		&fakeActorStore{}, &fakeExecutor{}, nil, nil, settingsStore, InstanceInfo{}, nil, nil, 0,
	)
	recorder := httptest.NewRecorder()
	request := jsonRequest(http.MethodPatch, "/api/v1/settings", `{"theme":"dark"}`)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if settingsStore.accountID != "account-1" || settingsStore.account.Theme != "dark" {
		t.Fatalf("settings scope/value = %q %+v", settingsStore.accountID, settingsStore.account)
	}
}

func TestActorSettingsRejectCrossAccountActor(t *testing.T) {
	settingsStore := &fakeSettingsStore{}
	actorStore := &fakeActorStore{actors: []actors.Actor{{ID: "actor-2", OwnerAccountID: "account-2"}}}
	handler := NewHandlerWithServices(
		fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf-token"}},
		actorStore, &fakeExecutor{}, nil, nil, settingsStore, InstanceInfo{}, nil, nil, 0,
	)
	recorder := httptest.NewRecorder()
	request := jsonRequest(http.MethodPatch, "/api/v1/actors/actor-2/settings", `{"default_visibility":"home"}`)
	handler.ServeHTTP(recorder, request)
	assertError(t, recorder, http.StatusNotFound, "actor_not_found")
	if settingsStore.actorID != "" {
		t.Fatalf("settings store called for unauthorized actor %q", settingsStore.actorID)
	}
}

func TestAccountSettingsRejectForeignSelectedActor(t *testing.T) {
	settingsStore := &fakeSettingsStore{}
	actorStore := &fakeActorStore{actors: []actors.Actor{{ID: "actor-2", OwnerAccountID: "account-2"}}}
	handler := NewHandlerWithServices(
		fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf-token"}},
		actorStore, &fakeExecutor{}, nil, nil, settingsStore, InstanceInfo{}, nil, nil, 0,
	)
	recorder := httptest.NewRecorder()
	request := jsonRequest(http.MethodPatch, "/api/v1/settings", `{"selected_actor_id":"actor-2"}`)
	handler.ServeHTTP(recorder, request)
	assertError(t, recorder, http.StatusNotFound, "actor_not_found")
	if settingsStore.accountID != "" {
		t.Fatal("settings store was called for a foreign selected Actor")
	}
}

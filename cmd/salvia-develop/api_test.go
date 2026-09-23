package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getJSON(t *testing.T, handler http.Handler, method, path string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var payload map[string]any
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %s returned invalid JSON: %v (%s)", method, path, err, recorder.Body.String())
		}
	}
	return recorder.Code, payload
}

func TestDummyAPIAuthenticatedBoot(t *testing.T) {
	handler := newDummyAPI(buildDemoData())

	status, setup := getJSON(t, handler, http.MethodGet, "/api/v1/auth/setup")
	if status != http.StatusOK {
		t.Fatalf("setup status = %d", status)
	}
	data, _ := setup["data"].(map[string]any)
	if required, _ := data["setup_required"].(bool); required {
		t.Fatal("development server must not require setup")
	}

	status, session := getJSON(t, handler, http.MethodGet, "/api/v1/session")
	if status != http.StatusOK {
		t.Fatalf("session status = %d", status)
	}
	sessionData, _ := session["data"].(map[string]any)
	if sessionData["account_id"] != demoAccountID || sessionData["csrf_token"] != demoCSRFToken {
		t.Fatalf("unexpected session: %#v", sessionData)
	}

	status, actors := getJSON(t, handler, http.MethodGet, "/api/v1/actors?limit=100")
	if status != http.StatusOK {
		t.Fatalf("actors status = %d", status)
	}
	list, _ := actors["data"].([]any)
	if len(list) == 0 {
		t.Fatal("expected at least one owned actor")
	}

	status, settings := getJSON(t, handler, http.MethodGet, "/api/v1/settings")
	if status != http.StatusOK {
		t.Fatalf("settings status = %d", status)
	}
	settingsData, _ := settings["data"].(map[string]any)
	if settingsData["selected_actor_id"] != demoSelectedActor {
		t.Fatalf("unexpected selected actor: %#v", settingsData)
	}
}

func TestDummyAPIUsesVersionedEnvelope(t *testing.T) {
	handler := newDummyAPI(buildDemoData())
	for _, path := range []string{
		"/api/v1/session",
		"/api/v1/actors?limit=100",
		"/api/v1/timelines/home?actor_id=" + demoSelectedActor + "&limit=30",
		"/api/v1/notifications?limit=50",
		"/api/v1/actors/" + demoSelectedActor + "/notifications?limit=50",
		"/api/v1/profiles/" + demoSelectedActor,
		"/api/v1/notes/dev-note-1",
		"/api/v1/emojis?scope=local&limit=100",
		"/api/v1/settings",
	} {
		status, payload := getJSON(t, handler, http.MethodGet, path)
		if status != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, status)
		}
		if payload["version"] != float64(1) {
			t.Fatalf("GET %s version = %#v", path, payload["version"])
		}
		if _, ok := payload["data"]; !ok {
			t.Fatalf("GET %s is missing data", path)
		}
	}
}

func TestDummyAPINotesIncludeMentionActors(t *testing.T) {
	handler := newDummyAPI(buildDemoData())
	status, payload := getJSON(t, handler, http.MethodGet, "/api/v1/notes/dev-note-1")
	if status != http.StatusOK {
		t.Fatalf("note status = %d", status)
	}
	data, _ := payload["data"].(map[string]any)
	mentions, _ := data["mentions"].([]any)
	if len(mentions) != 2 {
		t.Fatalf("mentions = %#v", data["mentions"])
	}
	local, _ := mentions[0].(map[string]any)
	if local["username"] != "thyme" || local["host"] != "" {
		t.Fatalf("local mention = %#v", local)
	}
	remote, _ := mentions[1].(map[string]any)
	if remote["username"] != "mint" || remote["host"] != "mint.example" {
		t.Fatalf("remote mention = %#v", remote)
	}
}

func TestDummyAPIMutationsDoNotChangeData(t *testing.T) {
	handler := newDummyAPI(buildDemoData())

	_, before := getJSON(t, handler, http.MethodGet, "/api/v1/timelines/home?actor_id="+demoSelectedActor+"&limit=30")

	if status, _ := getJSON(t, handler, http.MethodPut, "/api/v1/actors/"+demoSelectedActor+"/reactions/dev-note-1"); status != http.StatusOK {
		t.Fatalf("reaction status = %d", status)
	}
	if status, _ := getJSON(t, handler, http.MethodPatch, "/api/v1/actors/"+demoSelectedActor+"/notifications"); status != http.StatusOK {
		t.Fatalf("notifications status = %d", status)
	}
	if status, _ := getJSON(t, handler, http.MethodPost, "/api/v1/actors/"+demoSelectedActor+"/posts"); status != http.StatusOK {
		t.Fatalf("post status = %d", status)
	}

	_, after := getJSON(t, handler, http.MethodGet, "/api/v1/timelines/home?actor_id="+demoSelectedActor+"&limit=30")
	beforeJSON, _ := json.Marshal(before["data"])
	afterJSON, _ := json.Marshal(after["data"])
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("timeline changed after a mutation; development data must be immutable")
	}
}

func TestDummyAPILogoutReturnsNoContent(t *testing.T) {
	handler := newDummyAPI(buildDemoData())
	status, _ := getJSON(t, handler, http.MethodPost, "/api/v1/auth/logout")
	if status != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", status, http.StatusNoContent)
	}
}

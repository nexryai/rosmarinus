package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type fakeActorLookup struct {
	actor *actors.Actor
}

func (f fakeActorLookup) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	_ = ctx
	if f.actor != nil && f.actor.ID == id {
		return f.actor, nil
	}
	return nil, nil
}

func (f fakeActorLookup) FindLocalByUsername(ctx context.Context, username string) (*actors.Actor, error) {
	_ = ctx
	if f.actor != nil && strings.EqualFold(f.actor.Username, username) {
		return f.actor, nil
	}
	return nil, nil
}

func testConfig() config.Config {
	cfg, err := config.Load(func(key string) (string, bool) {
		switch key {
		case "HOST":
			return "example.test", true
		case "PUBLIC_URL":
			return "https://example.test", true
		default:
			return "", false
		}
	})
	if err != nil {
		panic(err)
	}
	return cfg
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	NewHandler(testConfig(), nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "ok\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHostMeta(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/host-meta", nil)
	rec := httptest.NewRecorder()
	NewHandler(testConfig(), nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "https://example.test/.well-known/webfinger?resource={uri}") {
		t.Fatalf("unexpected host-meta body: %s", rec.Body.String())
	}
}

func TestNodeInfoWellKnown(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/nodeinfo", nil)
	rec := httptest.NewRecorder()
	NewHandler(testConfig(), nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/nodeinfo/2.0") {
		t.Fatalf("unexpected nodeinfo body: %s", rec.Body.String())
	}
}

func TestWebFingerNotFoundWithoutLookup(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/webfinger?resource=acct:alice@example.test", nil)
	rec := httptest.NewRecorder()
	NewHandler(testConfig(), nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestWebFingerJRD(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/webfinger?resource=acct:alice@example.test", nil)
	rec := httptest.NewRecorder()
	lookup := fakeActorLookup{actor: &actors.Actor{
		ID:       "actor-id",
		Username: "alice",
		URI:      "https://example.test/users/actor-id",
	}}
	NewHandler(testConfig(), nil, lookup).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"subject":"acct:alice@example.test"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"href":"https://example.test/users/actor-id"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestWebFingerRejectsRemoteHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/webfinger?resource=acct:alice@remote.example", nil)
	rec := httptest.NewRecorder()
	NewHandler(testConfig(), nil, fakeActorLookup{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
}

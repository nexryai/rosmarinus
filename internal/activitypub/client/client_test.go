package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

func TestFetchObjectValidatesActivityResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = fmt.Fprintf(w, `{"@context":["%s"],"id":%q,"type":"Person"}`, activityStreamsContext, serverURL(r))
	}))
	defer server.Close()

	target := server.URL + "/actor"
	object, err := New(config.Config{}, server.Client()).FetchObject(context.Background(), target, nil)
	if err != nil {
		t.Fatalf("FetchObject returned error: %v", err)
	}
	if object["id"] != target {
		t.Fatalf("unexpected object: %+v", object)
	}
}

func TestFetchObjectRejectsInvalidContext(t *testing.T) {
	server := activityServer(t, func(r *http.Request) string {
		return fmt.Sprintf(`{"id":%q,"type":"Person"}`, serverURL(r))
	})
	defer server.Close()

	_, err := New(config.Config{}, server.Client()).FetchObject(context.Background(), server.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "@context") {
		t.Fatalf("expected @context error, got %v", err)
	}
}

func TestFetchObjectRejectsMismatchedID(t *testing.T) {
	server := activityServer(t, func(*http.Request) string {
		return fmt.Sprintf(`{"@context":%q,"id":"https://other.example/actor","type":"Person"}`, activityStreamsContext)
	})
	defer server.Close()

	_, err := New(config.Config{}, server.Client()).FetchObject(context.Background(), server.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected id mismatch, got %v", err)
	}
}

func TestFetchObjectRejectsNonCanonicalRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/actor" {
			http.Redirect(w, r, "/canonical", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = fmt.Fprintf(w, `{"@context":%q,"id":%q,"type":"Person"}`, activityStreamsContext, serverURL(r))
	}))
	defer server.Close()

	_, err := New(config.Config{}, server.Client()).FetchObject(context.Background(), server.URL+"/actor", nil)
	if err == nil || !strings.Contains(err.Error(), "request url") {
		t.Fatalf("expected request URL mismatch, got %v", err)
	}
}

func TestFetchObjectRejectsBlockedHostAndSubdomain(t *testing.T) {
	client := New(config.Config{FederationBlockedHosts: []string{"example.com"}}, nil)
	for _, target := range []string{"https://example.com/actor", "https://social.example.com/actor"} {
		if _, err := client.FetchObject(context.Background(), target, nil); err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("FetchObject(%q) should reject blocked host, got %v", target, err)
		}
	}
	if _, err := client.Deliver(context.Background(), "https://example.com/inbox", actors.Actor{}, map[string]any{}); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("Deliver should reject blocked host, got %v", err)
	}
}

func TestFetchObjectRejectsRedirectToBlockedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://social.blocked.example/actor", http.StatusFound)
	}))
	defer server.Close()

	client := New(config.Config{FederationBlockedHosts: []string{"BLOCKED.EXAMPLE."}}, server.Client())
	_, err := client.FetchObject(context.Background(), server.URL+"/actor", nil)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked redirect error, got %v", err)
	}
}

func TestFetchObjectRejectsFragment(t *testing.T) {
	client := New(config.Config{}, nil)
	if _, err := client.FetchObject(context.Background(), "https://remote.example/actor#key", nil); err == nil || !strings.Contains(err.Error(), "fragment") {
		t.Fatalf("expected fragment error, got %v", err)
	}
}

func activityServer(t *testing.T, body func(*http.Request) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/activity+json")
		_, _ = w.Write([]byte(body(r)))
	}))
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host + r.URL.Path
}

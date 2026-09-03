package salvia

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexAndHistoryFallback(t *testing.T) {
	handler := NewHandler()
	for _, requestPath := range []string{"/", "/settings", "/profiles/actor-1"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		request.Header.Set("Accept", "text/html")
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<title>Salvia</title>") {
			t.Fatalf("path=%s status=%d body=%q", requestPath, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("path=%s cache-control=%q", requestPath, recorder.Header().Get("Cache-Control"))
		}
	}
}

func TestHandlerServesHashedAssetsWithImmutableCaching(t *testing.T) {
	handler := NewHandler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, assetPath(t), nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control=%q", recorder.Header().Get("Cache-Control"))
	}
	if recorder.Header().Get("Content-Type") == "" {
		t.Fatal("asset response omitted Content-Type")
	}
}

func TestHandlerDoesNotFallbackForMissingAssetsOrFederationRequests(t *testing.T) {
	handler := NewHandler()
	tests := []struct {
		path   string
		accept string
	}{
		{path: "/assets/missing.js", accept: "text/html"},
		{path: "/missing.js", accept: "text/html"},
		{path: "/", accept: "application/activity+json"},
		{path: "/unknown-object", accept: "application/activity+json"},
		{path: "/unknown-object", accept: "application/json"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Accept", test.accept)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path=%s accept=%s status=%d", test.path, test.accept, recorder.Code)
		}
	}
}

func TestHandlerSecurityHeadersAndMethods(t *testing.T) {
	handler := NewHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", recorder.Code)
	}
	for _, header := range []string{"Content-Security-Policy", "Referrer-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if recorder.Header().Get(header) == "" {
			t.Fatalf("missing %s", header)
		}
	}
}

func assetPath(t *testing.T) string {
	t.Helper()
	root, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		t.Fatalf("open embedded dist: %v", err)
	}
	entries, err := fs.ReadDir(root, "assets")
	if err != nil || len(entries) == 0 {
		t.Fatalf("read embedded assets: entries=%d err=%v", len(entries), err)
	}
	return "/assets/" + entries[0].Name()
}

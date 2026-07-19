package webfinger

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveActorFromURL(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource := server.URL + "/users/alice"
		if r.URL.Path != "/.well-known/webfinger" || r.URL.Query().Get("resource") != resource {
			t.Fatalf("unexpected request URL: %s", r.URL.String())
		}
		if got := r.Header.Get("Accept"); got != "application/jrd+json, application/json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "rosmarinus-test/1.0" {
			t.Fatalf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/jrd+json; charset=utf-8")
		fmt.Fprintf(w, `{"subject":%q,"links":[{"rel":"SELF","href":%q}]}`, resource, server.URL+"/actors/alice")
	}))
	defer server.Close()

	actorURI, err := New(server.Client(), "rosmarinus-test/1.0").ResolveActor(context.Background(), server.URL+"/users/alice")
	if err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if actorURI != server.URL+"/actors/alice" {
		t.Fatalf("actor URI = %q", actorURI)
	}
}

func TestResolveActorHandleUsesHTTPS(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.URL.String(); got != "https://remote.example/.well-known/webfinger?resource=acct%3AAlice%40remote.example" {
			t.Fatalf("request URL = %q", got)
		}
		return response(http.StatusOK, "application/json", `{"subject":"acct:Alice@remote.example","links":[{"rel":"self","href":"https://remote.example/users/alice"}]}`), nil
	})

	actorURI, err := New(&http.Client{Transport: transport}, "").ResolveActor(context.Background(), "Alice@remote.example")
	if err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if actorURI != "https://remote.example/users/alice" {
		t.Fatalf("actor URI = %q", actorURI)
	}
}

func TestResolveActorAllowsCanonicalSubject(t *testing.T) {
	client := New(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "application/jrd+json", `{"subject":"acct:bob@remote.example","links":[{"rel":"self","href":"https://remote.example/users/alice"}]}`), nil
	})}, "")

	actorURI, err := client.ResolveActor(context.Background(), "alice@remote.example")
	if err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if actorURI != "https://remote.example/users/alice" {
		t.Fatalf("actor URI = %q", actorURI)
	}
}

func TestResolveActorRejectsMissingSelfLink(t *testing.T) {
	client := New(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "application/json", `{"subject":"acct:alice@remote.example","links":[]}`), nil
	})}, "")

	_, err := client.ResolveActor(context.Background(), "acct:alice@remote.example")
	if err == nil || !strings.Contains(err.Error(), "self link not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveActorLimitsResponseSize(t *testing.T) {
	client := New(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "application/json", strings.Repeat("x", maxResponseSize+1)), nil
	})}, "")

	_, err := client.ResolveActor(context.Background(), "alice@remote.example")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveActorRejectsInvalidQuery(t *testing.T) {
	client := New(nil, "")
	for _, query := range []string{"", "alice", "alice@", "https://user:password@remote.example/users/alice"} {
		t.Run(query, func(t *testing.T) {
			if _, err := client.ResolveActor(context.Background(), query); err == nil {
				t.Fatalf("ResolveActor(%q) should fail", query)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func response(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

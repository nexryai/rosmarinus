package instancemetadata

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchCombinesNodeInfoHTMLAndManifest(t *testing.T) {
	responses := map[string]string{
		"https://remote.example/.well-known/nodeinfo": `{"links":[{"rel":"http://nodeinfo.diaspora.software/ns/schema/2.0","href":"https://remote.example/nodeinfo/2.0"},{"rel":"http://nodeinfo.diaspora.software/ns/schema/2.1","href":"https://remote.example/nodeinfo/2.1"}]}`,
		"https://remote.example/nodeinfo/2.1":         `{"software":{"name":"Misskey","version":"2026.8.0"},"openRegistrations":true,"usage":{"users":{"total":12},"localPosts":30,"localComments":4},"metadata":{"nodeName":"Remote Node","nodeDescription":"NodeInfo description","maintainer":{"name":"Alice","email":"admin@example.test"}}}`,
		"https://remote.example":                      `<html><head><meta name="theme-color" content="#123456"><link rel="manifest" href="/assets/manifest.json"><link rel="icon" href="/favicon.png"></head></html>`,
		"https://remote.example/assets/manifest.json": `{"name":"Manifest Name","description":"Manifest description","icons":[{"src":"icon.png"}]}`,
	}
	requested := make([]string, 0, len(responses))
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		body, ok := responses[req.URL.String()]
		if !ok {
			return response(req, http.StatusNotFound, ""), nil
		}
		return response(req, http.StatusOK, body), nil
	})}

	metadata, err := New(time.Second, "test-agent", nil, client).Fetch(context.Background(), "remote.example")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if !metadata.NodeInfoFetched || metadata.SoftwareName != "Misskey" || metadata.SoftwareVersion != "2026.8.0" {
		t.Fatalf("unexpected software metadata: %+v", metadata)
	}
	if metadata.UsersCount != 12 || metadata.NotesCount != 34 || metadata.OpenRegistrations == nil || !*metadata.OpenRegistrations {
		t.Fatalf("unexpected NodeInfo counts: %+v", metadata)
	}
	if metadata.Name != "Remote Node" || metadata.Description != "NodeInfo description" || metadata.ThemeColor != "#123456" {
		t.Fatalf("unexpected presentation metadata: %+v", metadata)
	}
	if metadata.IconURL != "https://remote.example/assets/icon.png" || metadata.FaviconURL != "https://remote.example/favicon.png" {
		t.Fatalf("unexpected icon metadata: %+v", metadata)
	}
	if !containsString(requested, "https://remote.example/nodeinfo/2.1") || containsString(requested, "https://remote.example/nodeinfo/2.0") {
		t.Fatalf("NodeInfo preference was not 2.1: %v", requested)
	}
}

func TestFetchUsesFixedManifestAndFaviconFallback(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/.well-known/nodeinfo":
			return response(req, http.StatusNotFound, ""), nil
		case (req.URL.Path == "" || req.URL.Path == "/") && req.Method == http.MethodGet:
			return response(req, http.StatusOK, `<meta property="og:title" content="HTML Name">`), nil
		case req.URL.Path == "/manifest.json":
			return response(req, http.StatusOK, `{"icons":[{"src":"/icon.webp"}]}`), nil
		case req.URL.Path == "/favicon.ico" && req.Method == http.MethodHead:
			return response(req, http.StatusNoContent, ""), nil
		default:
			return response(req, http.StatusNotFound, ""), nil
		}
	})}

	metadata, err := New(time.Second, "test-agent", nil, client).Fetch(context.Background(), "remote.example")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if metadata.NodeInfoFetched || metadata.Name != "HTML Name" || metadata.IconURL != "https://remote.example/icon.webp" || metadata.FaviconURL != "https://remote.example/favicon.ico" {
		t.Fatalf("unexpected fallback metadata: %+v", metadata)
	}
}

func TestGetJSONRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, strings.Repeat("x", maxMetadataResponseSize+1)), nil
	})}
	fetcher := New(time.Second, "test-agent", nil, client)
	if err := fetcher.getJSON(context.Background(), mustURL(t, "https://remote.example/data"), &map[string]any{}); err == nil {
		t.Fatal("oversized metadata response was accepted")
	}
}

func response(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	target, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

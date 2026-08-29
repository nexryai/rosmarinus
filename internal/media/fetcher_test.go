package mediafetch

import (
	"context"
	"errors"
	"io"
	"net"
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

func TestValidateURLRejectsNonPublicTargets(t *testing.T) {
	tests := []string{
		"http://remote.example/file.png",
		"https://localhost/file.png",
		"https://127.0.0.1/file.png",
		"https://10.0.0.1/file.png",
		"https://100.64.0.1/file.png",
		"https://192.0.2.1/file.png",
		"https://[::1]/file.png",
		"https://[2001:db8::1]/file.png",
		"https://user:password@remote.example/file.png",
		"https://remote.example/file.png#fragment",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			target, err := url.Parse(rawURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateURL(target); err == nil {
				t.Fatalf("ValidateURL(%q) succeeded", rawURL)
			}
		})
	}
	target, _ := url.Parse("https://remote.example/file.png")
	if err := ValidateURL(target); err != nil {
		t.Fatalf("public HTTPS URL was rejected: %v", err)
	}
}

func TestFetcherAllowsExplicitPrivateNetwork(t *testing.T) {
	target, _ := url.Parse("https://10.0.0.5/file.png")
	if err := NewWithAllowedNetworks(1024, time.Second, "", []string{"10.0.0.0/8"}, &http.Client{}).ValidateURL(target); err != nil {
		t.Fatalf("explicitly allowed target was rejected: %v", err)
	}
}

func TestSafeDialerRejectsHostnameResolvingToPrivateAddress(t *testing.T) {
	dialed := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected dial")
	}
	_, err := safeDialer(dial, nil, "test")(context.Background(), "tcp", "localhost:443")
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("private DNS result was not rejected: %v", err)
	}
	if dialed {
		t.Fatal("network dial occurred before DNS safety validation")
	}
}

func TestFetcherAcceptsBoundedRasterImageAndSetsUserAgent(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("User-Agent") != "rosmarinus-test" {
			t.Fatalf("User-Agent = %q", req.Header.Get("User-Agent"))
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"image/png"}},
			Body:          io.NopCloser(strings.NewReader(string(png))),
			ContentLength: int64(len(png)),
			Request:       req,
		}, nil
	})}
	result, err := New(1024, time.Second, "rosmarinus-test", client).Fetch(context.Background(), "https://remote.example/file.png")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.ContentType != "image/png" || string(result.Body) != string(png) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFetcherRejectsOversizedAndActivePayloads(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		maxBytes    int64
	}{
		{name: "oversized", contentType: "image/png", body: strings.Repeat("x", 9), maxBytes: 8},
		{name: "html disguised as image", contentType: "image/png", body: "<!doctype html><html></html>", maxBytes: 1024},
		{name: "svg", contentType: "image/svg+xml", body: "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>", maxBytes: 1024},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{tt.contentType}},
					Body:       io.NopCloser(strings.NewReader(tt.body)),
					Request:    req,
				}, nil
			})}
			if _, err := New(tt.maxBytes, time.Second, "", client).Fetch(context.Background(), "https://remote.example/file"); err == nil {
				t.Fatal("Fetch succeeded")
			}
		})
	}
}

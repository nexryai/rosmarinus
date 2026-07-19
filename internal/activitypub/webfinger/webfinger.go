package webfinger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseSize = 64 * 1024

type Client struct {
	httpClient *http.Client
	userAgent  string
}

type Document struct {
	Subject string `json:"subject"`
	Links   []Link `json:"links"`
}

type Link struct {
	Rel  string `json:"rel"`
	Type string `json:"type,omitempty"`
	Href string `json:"href"`
}

func New(httpClient *http.Client, userAgent string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{httpClient: httpClient, userAgent: userAgent}
}

func (c *Client) ResolveActor(ctx context.Context, query string) (string, error) {
	endpoint, _, err := endpointFor(query)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create webfinger request: %w", err)
	}
	req.Header.Set("Accept", "application/jrd+json, application/json")
	if strings.TrimSpace(c.userAgent) != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("webfinger request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("webfinger status %d", res.StatusCode)
	}
	if !validContentType(res.Header.Get("Content-Type")) {
		return "", fmt.Errorf("invalid webfinger content-type: %s", res.Header.Get("Content-Type"))
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseSize+1))
	if err != nil {
		return "", fmt.Errorf("read webfinger response: %w", err)
	}
	if len(raw) > maxResponseSize {
		return "", fmt.Errorf("webfinger response is too large")
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", fmt.Errorf("decode webfinger response: %w", err)
	}
	if strings.TrimSpace(document.Subject) == "" {
		return "", fmt.Errorf("webfinger subject is required")
	}
	for _, link := range document.Links {
		if !strings.EqualFold(strings.TrimSpace(link.Rel), "self") {
			continue
		}
		href, err := parseHTTPURL(link.Href)
		if err != nil {
			return "", fmt.Errorf("invalid webfinger self link: %w", err)
		}
		return href.String(), nil
	}
	return "", fmt.Errorf("webfinger self link not found")
}

func endpointFor(query string) (string, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", "", fmt.Errorf("webfinger query is required")
	}
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		target, err := parseHTTPURL(query)
		if err != nil {
			return "", "", fmt.Errorf("invalid webfinger URL query: %w", err)
		}
		endpoint := &url.URL{Scheme: target.Scheme, Host: target.Host, Path: "/.well-known/webfinger"}
		values := endpoint.Query()
		values.Set("resource", query)
		endpoint.RawQuery = values.Encode()
		return endpoint.String(), query, nil
	}

	handle := strings.TrimPrefix(query, "acct:")
	parts := strings.Split(handle, "@")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid webfinger handle: %s", query)
	}
	hostURL, err := parseHTTPURL("https://" + parts[1])
	if err != nil || hostURL.Path != "" || hostURL.RawQuery != "" || hostURL.Fragment != "" {
		return "", "", fmt.Errorf("invalid webfinger handle: %s", query)
	}
	resource := "acct:" + handle
	endpoint := &url.URL{Scheme: "https", Host: hostURL.Host, Path: "/.well-known/webfinger"}
	values := endpoint.Query()
	values.Set("resource", resource)
	endpoint.RawQuery = values.Encode()
	return endpoint.String(), resource, nil
}

func parseHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("URL credentials are not allowed")
	}
	return parsed, nil
}

func validContentType(value string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	return base == "application/jrd+json" || base == "application/json"
}

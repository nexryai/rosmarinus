package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
)

const maxActivityResponseSize = 64 * 1024

const activityStreamsContext = "https://www.w3.org/ns/activitystreams"

type Client struct {
	httpClient         *http.Client
	validateNetworkURL func(*url.URL) error
	cfg                config.Config
}

type StatusError struct {
	Operation string
	Status    int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s status %d", e.Operation, e.Status)
}

func (e *StatusError) HTTPStatusCode() int {
	return e.Status
}

func New(cfg config.Config, httpClient *http.Client) *Client {
	httpClient, validateNetworkURL := mediafetch.NewSafeFederationHTTPClient(
		30*time.Second,
		cfg.UserAgent,
		cfg.MediaAllowedPrivateNetworks,
		httpClient,
	)
	client := &Client{cfg: cfg, validateNetworkURL: validateNetworkURL}
	clone := *httpClient
	previousRedirectPolicy := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if err := client.validateFederationURL(req.URL); err != nil {
			return err
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("activitypub redirect cannot downgrade https to %s", req.URL.Scheme)
		}
		if len(via) > 0 && via[0].Method == http.MethodPost {
			return http.ErrUseLastResponse
		}
		if previousRedirectPolicy != nil {
			return previousRedirectPolicy(req, via)
		}
		return nil
	}
	client.httpClient = &clone
	return client
}

func (c *Client) FetchObject(ctx context.Context, targetURL string, signer *actors.Actor) (map[string]any, error) {
	requestedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid activitypub url: %w", err)
	}
	if err := c.validateFederationURL(requestedURL); err != nil {
		return nil, err
	}
	req, err := c.newGetRequest(ctx, targetURL, signer)
	if err != nil {
		return nil, err
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("activitypub get: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &StatusError{Operation: "activitypub get", Status: res.StatusCode}
	}
	if !validActivityContentType(res.Header.Get("Content-Type")) {
		return nil, fmt.Errorf("invalid activitypub content-type: %s", res.Header.Get("Content-Type"))
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxActivityResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read activitypub response: %w", err)
	}
	if len(raw) > maxActivityResponseSize {
		return nil, fmt.Errorf("activitypub response is too large")
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode activitypub response: %w", err)
	}
	if err := validateActivityResponse(requestedURL, res.Request.URL, object); err != nil {
		return nil, err
	}
	return object, nil
}

func (c *Client) Deliver(ctx context.Context, targetURL string, signer actors.Actor, object map[string]any) (int, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return 0, fmt.Errorf("invalid activitypub url: %w", err)
	}
	if err := c.validateFederationURL(target); err != nil {
		return 0, err
	}
	body, err := json.Marshal(object)
	if err != nil {
		return 0, fmt.Errorf("marshal activitypub delivery: %w", err)
	}
	signed, err := apsig.CreateSignedPost(privateKey(signer), targetURL, body, map[string]string{
		"User-Agent": c.cfg.UserAgent,
	}, time.Now())
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, signed.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	applySignedHeaders(req, signed.Headers)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("activitypub deliver: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, &StatusError{Operation: "activitypub deliver", Status: res.StatusCode}
	}
	return res.StatusCode, nil
}

func (c *Client) newGetRequest(ctx context.Context, targetURL string, signer *actors.Actor) (*http.Request, error) {
	if signer == nil || signer.PrivateKeyPEM == "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", apsig.ActivityAccept)
		req.Header.Set("User-Agent", c.cfg.UserAgent)
		return req, nil
	}
	signed, err := apsig.CreateSignedGet(privateKey(*signer), targetURL, map[string]string{
		"User-Agent": c.cfg.UserAgent,
	}, time.Now())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, signed.Method, targetURL, nil)
	if err != nil {
		return nil, err
	}
	applySignedHeaders(req, signed.Headers)
	return req, nil
}

func privateKey(actor actors.Actor) apsig.PrivateKey {
	return apsig.PrivateKey{
		KeyID:         actor.PublicKeyID,
		PrivateKeyPEM: actor.PrivateKeyPEM,
	}
}

func applySignedHeaders(req *http.Request, headers map[string]string) {
	for key, value := range headers {
		if strings.EqualFold(key, "host") {
			req.Host = value
			continue
		}
		req.Header.Set(key, value)
	}
}

func validActivityContentType(contentType string) bool {
	parts := strings.Split(contentType, ";")
	if len(parts) == 0 {
		return false
	}
	base := strings.TrimSpace(strings.ToLower(parts[0]))
	if base == "application/activity+json" {
		return true
	}
	if base != "application/ld+json" {
		return false
	}
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == `profile="https://www.w3.org/ns/activitystreams"` {
			return true
		}
	}
	return false
}

func (c *Client) validateFederationURL(target *url.URL) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return fmt.Errorf("activitypub url must be an absolute http or https url")
	}
	if target.Fragment != "" {
		return fmt.Errorf("activitypub url must not contain a fragment")
	}
	if c.validateNetworkURL != nil {
		if err := c.validateNetworkURL(target); err != nil {
			return err
		}
	}
	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if c.cfg.IsFederationHostBlocked(host) {
		return fmt.Errorf("activitypub host is blocked: %s", host)
	}
	return nil
}

func validateActivityResponse(requestedURL, finalURL *url.URL, object map[string]any) error {
	if !hasActivityStreamsContext(object["@context"]) {
		return fmt.Errorf("activitypub response has invalid @context")
	}
	id, ok := object["id"].(string)
	if !ok || id == "" {
		return fmt.Errorf("activitypub response is missing a string id")
	}
	idURL, err := url.Parse(id)
	if err != nil || (idURL.Scheme != "http" && idURL.Scheme != "https") || idURL.Host == "" || idURL.User != nil {
		return fmt.Errorf("activitypub response has invalid id")
	}
	request := normalizedActivityURL(requestedURL, true)
	final := normalizedActivityURL(finalURL, true)
	objectID := normalizedActivityURL(idURL, false)
	if requestedURL.Scheme == "https" && finalURL.Scheme != "https" {
		return fmt.Errorf("activitypub response downgraded https to %s", finalURL.Scheme)
	}
	if final != objectID {
		return fmt.Errorf("activitypub response id does not match final url")
	}
	if request != objectID {
		return fmt.Errorf("activitypub response id does not match request url")
	}
	return nil
}

func normalizedActivityURL(value *url.URL, clearFragment bool) string {
	copy := *value
	if clearFragment {
		copy.Fragment = ""
	}
	port := copy.Port()
	hostname := strings.TrimPrefix(strings.ToLower(copy.Hostname()), "www.")
	copy.Host = hostname
	if port != "" {
		copy.Host += ":" + port
	}
	return copy.String()
}

func hasActivityStreamsContext(value any) bool {
	if value == activityStreamsContext {
		return true
	}
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == activityStreamsContext {
			return true
		}
	}
	return false
}

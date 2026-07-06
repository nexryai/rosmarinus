package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

const maxActivityResponseSize = 64 * 1024

type Client struct {
	httpClient *http.Client
	cfg        config.Config
}

func New(cfg config.Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{cfg: cfg, httpClient: httpClient}
}

func (c *Client) FetchObject(ctx context.Context, targetURL string, signer *actors.Actor) (map[string]any, error) {
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
		return nil, fmt.Errorf("activitypub get status %d", res.StatusCode)
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
	return object, nil
}

func (c *Client) Deliver(ctx context.Context, targetURL string, signer actors.Actor, object map[string]any) error {
	body, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("marshal activitypub delivery: %w", err)
	}
	signed, err := apsig.CreateSignedPost(privateKey(signer), targetURL, body, map[string]string{
		"User-Agent": c.cfg.UserAgent,
	}, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, signed.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	applySignedHeaders(req, signed.Headers)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("activitypub deliver: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("activitypub deliver status %d", res.StatusCode)
	}
	return nil
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

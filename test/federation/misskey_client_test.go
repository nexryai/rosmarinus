package federation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type misskeyClient struct {
	t          *testing.T
	baseURL    string
	httpClient *http.Client
}

type misskeyAccount struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type misskeyDriveFile struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func newMisskeyClient(t *testing.T) *misskeyClient {
	t.Helper()
	return newMisskeyClientAt(t, "https://a.test")
}

func newMisskeyClientAt(t *testing.T, baseURL string) *misskeyClient {
	t.Helper()
	return &misskeyClient{t: t, baseURL: strings.TrimRight(baseURL, "/"), httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (m *misskeyClient) createAdmin(ctx context.Context, username, password string) misskeyAccount {
	var account misskeyAccount
	m.call(ctx, "admin/accounts/create", map[string]any{"username": username, "password": password}, &account)
	if account.ID == "" || account.Token == "" {
		m.t.Fatalf("Misskey admin creation returned incomplete account: %+v", account)
	}
	return account
}

func (m *misskeyClient) createAccount(ctx context.Context, adminToken, username, password string) misskeyAccount {
	var account misskeyAccount
	m.call(ctx, "admin/accounts/create", map[string]any{
		"i":        adminToken,
		"username": username,
		"password": password,
	}, &account)
	if account.ID == "" || account.Token == "" {
		m.t.Fatalf("Misskey account creation returned incomplete account: %+v", account)
	}
	return account
}

func (m *misskeyClient) uploadFile(ctx context.Context, token, filename string, data []byte) misskeyDriveFile {
	m.t.Helper()
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	if err := writer.WriteField("i", token); err != nil {
		m.t.Fatalf("write Misskey upload token: %v", err)
	}
	if err := writer.WriteField("force", "true"); err != nil {
		m.t.Fatalf("write Misskey upload option: %v", err)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		m.t.Fatalf("create Misskey upload part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		m.t.Fatalf("write Misskey upload file: %v", err)
	}
	if err := writer.Close(); err != nil {
		m.t.Fatalf("close Misskey upload body: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/drive/files/create", &requestBody)
	if err != nil {
		m.t.Fatalf("create Misskey upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("upload Misskey file: %v", err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read Misskey upload response: %v", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		m.t.Fatalf("Misskey file upload status=%d body=%s", res.StatusCode, loggableMisskeyResponse(responseBody))
	}
	var file misskeyDriveFile
	if err := json.Unmarshal(responseBody, &file); err != nil || file.ID == "" {
		m.t.Fatalf("decode Misskey upload response: file=%+v err=%v", file, err)
	}
	return file
}

func (m *misskeyClient) call(ctx context.Context, endpoint string, payload map[string]any, result any) {
	m.t.Helper()
	status := m.callStatus(ctx, endpoint, payload, result)
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		m.t.Fatalf("Misskey %s status=%d", endpoint, status)
	}
}

func (m *misskeyClient) callStatus(ctx context.Context, endpoint string, payload map[string]any, result any) int {
	m.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		m.t.Fatalf("marshal Misskey %s request: %v", endpoint, err)
	}
	m.t.Logf("Misskey API request endpoint=%s body=%s", endpoint, loggableMisskeyResponse(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/"+endpoint, bytes.NewReader(body))
	if err != nil {
		m.t.Fatalf("create Misskey %s request: %v", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("call Misskey %s: %v", endpoint, err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read Misskey %s response: %v", endpoint, err)
	}
	logBody := loggableMisskeyResponse(responseBody)
	m.t.Logf("Misskey API response endpoint=%s status=%s body=%s", endpoint, res.Status, logBody)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode
	}
	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			m.t.Fatalf("decode Misskey %s response: %v body=%s", endpoint, err, logBody)
		}
	}
	return res.StatusCode
}

func (m *misskeyClient) get(ctx context.Context, uri string, result any) {
	m.t.Helper()
	m.t.Logf("ActivityPub GET request uri=%s", uri)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		m.t.Fatalf("create ActivityPub GET request: %v", err)
	}
	req.Header.Set("Accept", "application/activity+json")
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("ActivityPub GET %s: %v", uri, err)
	}
	defer res.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read ActivityPub GET response: %v", err)
	}
	m.t.Logf("ActivityPub GET response uri=%s status=%s body=%s", uri, res.Status, loggableMisskeyResponse(responseBody))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		m.t.Fatalf("ActivityPub GET %s status=%d body=%s", uri, res.StatusCode, loggableMisskeyResponse(responseBody))
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		m.t.Fatalf("decode ActivityPub GET response: %v", err)
	}
}

func (m *misskeyClient) getStatus(ctx context.Context, uri string) int {
	m.t.Helper()
	m.t.Logf("ActivityPub status request uri=%s", uri)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		m.t.Fatalf("create ActivityPub status request: %v", err)
	}
	req.Header.Set("Accept", "application/activity+json")
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("ActivityPub status request %s: %v", uri, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	m.t.Logf("ActivityPub status response uri=%s status=%s", uri, res.Status)
	return res.StatusCode
}

func (m *misskeyClient) getRaw(ctx context.Context, uri string) (int, string, []byte) {
	m.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		m.t.Fatalf("create raw GET request: %v", err)
	}
	res, err := m.httpClient.Do(req)
	if err != nil {
		m.t.Fatalf("raw GET %s: %v", uri, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		m.t.Fatalf("read raw GET %s: %v", uri, err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), body
}

func loggableMisskeyResponse(body []byte) string {
	if len(body) == 0 {
		return "<empty>"
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return string(body)
	}
	redactMisskeySecrets(value)
	var redacted bytes.Buffer
	encoder := json.NewEncoder(&redacted)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "<unavailable>"
	}
	return strings.TrimSpace(redacted.String())
}

func redactMisskeySecrets(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "token", "accesstoken", "secret", "password", "i":
				typed[key] = "<redacted>"
			default:
				redactMisskeySecrets(child)
			}
		}
	case []any:
		for _, child := range typed {
			redactMisskeySecrets(child)
		}
	}
}

func waitFor(t *testing.T, ctx context.Context, description string, ready func() bool) {
	t.Helper()
	startedAt := time.Now()
	attempts := 0
	t.Logf("waiting for %s", description)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		attempts++
		if ready() {
			t.Logf("wait completed description=%q attempts=%d elapsed=%s", description, attempts, time.Since(startedAt).Round(time.Millisecond))
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: %v", description, ctx.Err())
		case <-ticker.C:
		}
	}
}

func envRequired(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func assertObjectIDString(t *testing.T, name, id string) {
	t.Helper()
	if id != strings.ToLower(id) {
		t.Fatalf("%s id is not lowercase: %q", name, id)
	}
	if _, err := bson.ObjectIDFromHex(id); err != nil {
		t.Fatalf("%s id is not an ObjectID string: %q: %v", name, id, err)
	}
}

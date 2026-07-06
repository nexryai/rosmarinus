package httpserver

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeActorLookup struct {
	actor *actors.Actor
}

type fakeQueueClient struct {
	task queue.Task
}

func (f *fakeQueueClient) Enqueue(ctx context.Context, task queue.Task) error {
	_ = ctx
	f.task = task
	return nil
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
	NewHandler(testConfig(), nil, nil, nil).ServeHTTP(rec, req)
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
	NewHandler(testConfig(), nil, nil, nil).ServeHTTP(rec, req)
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
	NewHandler(testConfig(), nil, nil, nil).ServeHTTP(rec, req)
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
	NewHandler(testConfig(), nil, nil, nil).ServeHTTP(rec, req)
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
	NewHandler(testConfig(), nil, lookup, nil).ServeHTTP(rec, req)
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
	NewHandler(testConfig(), nil, fakeActorLookup{}, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestActorByID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/actor-id", nil)
	rec := httptest.NewRecorder()
	lookup := fakeActorLookup{actor: &actors.Actor{
		ID:           "actor-id",
		Username:     "alice",
		Name:         "Alice",
		Type:         "Service",
		URI:          "https://example.test/users/actor-id",
		Inbox:        "https://example.test/users/actor-id/inbox",
		SharedInbox:  "https://example.test/inbox",
		PublicKeyID:  "https://example.test/users/actor-id#main-key",
		PublicKeyPEM: "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n",
	}}
	NewHandler(testConfig(), nil, lookup, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/activity+json") {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"preferredUsername":"alice"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"publicKeyPem":"-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n"`) {
		t.Fatalf("unexpected public key body: %s", rec.Body.String())
	}
}

func TestPublicKeyByID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/actor-id/publickey", nil)
	rec := httptest.NewRecorder()
	lookup := fakeActorLookup{actor: &actors.Actor{
		ID:           "actor-id",
		Username:     "alice",
		URI:          "https://example.test/users/actor-id",
		PublicKeyID:  "https://example.test/users/actor-id#main-key",
		PublicKeyPEM: "pem",
	}}
	NewHandler(testConfig(), nil, lookup, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"type":"Key"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestInboxEnqueuesAcceptedActivity(t *testing.T) {
	cfg := testConfig()
	body := []byte(`{"type":"Create","actor":"https://remote.example/users/alice","id":"https://remote.example/activities/1","object":{"type":"Note","id":"https://remote.example/notes/1"}}`)
	req := httptest.NewRequest(http.MethodPost, "https://example.test/inbox", strings.NewReader(string(body)))
	req.Host = cfg.Host
	req.Header.Set("Date", time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
	req.Header.Set("Digest", apsig.DigestHeader(body))
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	signingString, err := apsig.SigningString(apsig.Request{
		URL:    "https://" + req.Host + req.URL.RequestURI(),
		Method: req.Method,
		Headers: map[string]string{
			"Date":   req.Header.Get("Date"),
			"Host":   req.Host,
			"Digest": req.Header.Get("Digest"),
		},
	}, []string{"(request-target)", "digest", "host", "date"})
	if err != nil {
		t.Fatalf("SigningString returned error: %v", err)
	}
	sum := sha256.Sum256([]byte(signingString))
	rawSignature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15 returned error: %v", err)
	}
	req.Header.Set("Signature", `keyId="https://remote.example/users/alice#main-key",algorithm="rsa-sha256",headers="(request-target) digest host date",signature="`+base64.StdEncoding.EncodeToString(rawSignature)+`"`)
	rec := httptest.NewRecorder()
	q := &fakeQueueClient{}
	NewHandler(cfg, nil, nil, q).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if q.task.Type != queue.TaskInbox {
		t.Fatalf("task type = %q", q.task.Type)
	}
}

package httpserver

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeActorLookup struct {
	actor *actors.Actor
}

type fakeQueueClient struct {
	task queue.Task
}

type fakeNoteLookup struct {
	note *domainnotes.Note
}

type fakeFollowLookup struct {
	followers []follows.Follow
	following []follows.Follow
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

func (f fakeNoteLookup) FindByID(ctx context.Context, id string) (*domainnotes.Note, error) {
	_ = ctx
	if f.note != nil && f.note.ID == id {
		return f.note, nil
	}
	return nil, nil
}

func (f fakeFollowLookup) CountFollowers(ctx context.Context, id string) (int, error) {
	_ = ctx
	_ = id
	return len(f.followers), nil
}

func (f fakeFollowLookup) CountFollowing(ctx context.Context, id string) (int, error) {
	_ = ctx
	_ = id
	return len(f.following), nil
}

func (f fakeFollowLookup) ListFollowers(ctx context.Context, id string, limit int) ([]follows.Follow, error) {
	_ = ctx
	_ = id
	if limit > len(f.followers) {
		limit = len(f.followers)
	}
	return f.followers[:limit], nil
}

func (f fakeFollowLookup) ListFollowing(ctx context.Context, id string, limit int) ([]follows.Follow, error) {
	_ = ctx
	_ = id
	if limit > len(f.following) {
		limit = len(f.following)
	}
	return f.following[:limit], nil
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

func TestActorCollectionsByID(t *testing.T) {
	lookup := fakeActorLookup{actor: &actors.Actor{
		ID:       "actor-id",
		Username: "alice",
		URI:      "https://example.test/users/actor-id",
	}}
	cases := []struct {
		path string
		want []string
	}{
		{
			path: "/users/actor-id/outbox",
			want: []string{`"type":"OrderedCollection"`, `"id":"https://example.test/users/actor-id/outbox"`, `"first":"https://example.test/users/actor-id/outbox?page=true"`, `"last":"https://example.test/users/actor-id/outbox?page=true\u0026since_id=000000000000000000000000"`},
		},
		{
			path: "/users/actor-id/followers",
			want: []string{`"type":"OrderedCollection"`, `"id":"https://example.test/users/actor-id/followers"`, `"first":"https://example.test/users/actor-id/followers?page=true"`},
		},
		{
			path: "/users/actor-id/following",
			want: []string{`"type":"OrderedCollection"`, `"id":"https://example.test/users/actor-id/following"`, `"first":"https://example.test/users/actor-id/following?page=true"`},
		},
		{
			path: "/users/actor-id/outbox?page=true",
			want: []string{`"type":"OrderedCollectionPage"`, `"partOf":"https://example.test/users/actor-id/outbox"`, `"orderedItems":[]`},
		},
		{
			path: "/users/actor-id/collections/featured",
			want: []string{`"type":"OrderedCollection"`, `"id":"https://example.test/users/actor-id/collections/featured"`, `"orderedItems":[]`},
		},
	}
	for _, tt := range cases {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			NewHandler(testConfig(), nil, lookup, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(rec.Body.String(), want) {
					t.Fatalf("body does not contain %q: %s", want, rec.Body.String())
				}
			}
		})
	}
}

func TestActorFollowCollectionsByIDUseStoredFollows(t *testing.T) {
	lookup := fakeActorLookup{actor: &actors.Actor{
		ID:       "actor-id",
		Username: "alice",
		URI:      "https://example.test/users/actor-id",
	}}
	followLookup := fakeFollowLookup{
		followers: []follows.Follow{{
			FollowerID:  "remote-alice",
			FolloweeID:  "actor-id",
			FollowerURI: "https://remote.example/users/alice",
			FolloweeURI: "https://example.test/users/actor-id",
		}},
		following: []follows.Follow{{
			FollowerID:  "actor-id",
			FolloweeID:  "remote-bob",
			FollowerURI: "https://example.test/users/actor-id",
			FolloweeURI: "https://remote.example/users/bob",
		}},
	}
	cases := []struct {
		path string
		want []string
	}{
		{
			path: "/users/actor-id/followers",
			want: []string{`"totalItems":1`, `"first":"https://example.test/users/actor-id/followers?page=true"`},
		},
		{
			path: "/users/actor-id/followers?page=true",
			want: []string{`"totalItems":1`, `"orderedItems":["https://remote.example/users/alice"]`},
		},
		{
			path: "/users/actor-id/following",
			want: []string{`"totalItems":1`, `"first":"https://example.test/users/actor-id/following?page=true"`},
		},
		{
			path: "/users/actor-id/following?page=true",
			want: []string{`"totalItems":1`, `"orderedItems":["https://remote.example/users/bob"]`},
		},
	}
	for _, tt := range cases {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			NewHandlerWithStores(testConfig(), nil, lookup, nil, followLookup, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(rec.Body.String(), want) {
					t.Fatalf("body does not contain %q: %s", want, rec.Body.String())
				}
			}
		})
	}
}

func TestNoteByID(t *testing.T) {
	cw := "cw"
	req := httptest.NewRequest(http.MethodGet, "/notes/note-id", nil)
	req.Header.Set("Accept", "application/activity+json")
	rec := httptest.NewRecorder()
	noteLookup := fakeNoteLookup{note: &domainnotes.Note{
		ID:             "note-id",
		URI:            "https://remote.example/notes/1",
		AttributedTo:   "https://remote.example/users/alice",
		Text:           "hello",
		ContentWarning: &cw,
		InReplyToURI:   "https://remote.example/notes/root",
		QuoteURI:       "https://remote.example/notes/quote",
		Visibility:     domainnotes.VisibilityPublic,
		Hashtags:       []string{"hello"},
		MentionURIs:    []string{"https://remote.example/users/bob"},
		Attachments: []domainnotes.Attachment{{
			Type:      "Document",
			MediaType: "image/png",
			URL:       "https://remote.example/files/1.png",
			Name:      "file",
		}},
		CreatedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
	}}
	NewHandlerWithStores(testConfig(), nil, nil, noteLookup, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/activity+json") {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"id":"https://remote.example/notes/1"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"_misskey_content":"hello"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"summary":"cw"`) || !strings.Contains(rec.Body.String(), `"sensitive":true`) {
		t.Fatalf("unexpected cw body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"inReplyTo":"https://remote.example/notes/root"`) {
		t.Fatalf("unexpected reply body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"_misskey_quote":"https://remote.example/notes/quote"`) || !strings.Contains(rec.Body.String(), `"quoteUrl":"https://remote.example/notes/quote"`) {
		t.Fatalf("unexpected quote body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"type":"Hashtag"`) || !strings.Contains(rec.Body.String(), `"name":"#hello"`) {
		t.Fatalf("unexpected tag body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"type":"Mention"`) || !strings.Contains(rec.Body.String(), `"href":"https://remote.example/users/bob"`) {
		t.Fatalf("unexpected mention body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"attachment":[`) || !strings.Contains(rec.Body.String(), `"url":"https://remote.example/files/1.png"`) {
		t.Fatalf("unexpected attachment body: %s", rec.Body.String())
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

func TestConcordeInboxValidationCases(t *testing.T) {
	cfg := testConfig()
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	key := apsig.PrivateKey{
		KeyID:         "https://remote.example/users/a#main-key",
		PrivateKeyPEM: privateKeyPEM(privateKey),
	}
	cases := []struct {
		name       string
		body       string
		host       string
		mutate     func(map[string]string)
		wantStatus int
	}{
		{
			name:       "Accepted",
			body:       `{"a":1,"b":2}`,
			host:       cfg.Host,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "Invalid Host",
			body:       `{"a":1,"b":2}`,
			host:       "xxx.local",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Payload Too Large",
			body:       `{"a":1,"b":"` + strings.Repeat("x", 70000) + `"}`,
			host:       cfg.Host,
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "Signature Header Required",
			body: `{"a":1,"b":2}`,
			host: cfg.Host,
			mutate: func(headers map[string]string) {
				delete(headers, "signature")
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Digest Header Required",
			body: `{"a":1,"b":2}`,
			host: cfg.Host,
			mutate: func(headers map[string]string) {
				delete(headers, "digest")
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Invalid Digest Header",
			body: `{"a":1,"b":2}`,
			host: cfg.Host,
			mutate: func(headers map[string]string) {
				headers["digest"] = "puee"
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Unsupported Digest Algorithm",
			body: `{"a":1,"b":2}`,
			host: cfg.Host,
			mutate: func(headers map[string]string) {
				headers["digest"] = "SHA-5000=abc"
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "Digest Mismatch",
			body: `{"a":1,"b":2}`,
			host: cfg.Host,
			mutate: func(headers map[string]string) {
				headers["digest"] = apsig.DigestHeader([]byte("puppukupu-"))
			},
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			signed, err := apsig.CreateSignedPost(key, "https://example.test/inbox", []byte(tt.body), map[string]string{"Host": tt.host}, time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("CreateSignedPost returned error: %v", err)
			}
			headers := make(map[string]string, len(signed.Headers))
			for k, v := range signed.Headers {
				headers[k] = v
			}
			if tt.mutate != nil {
				tt.mutate(headers)
			}
			req := httptest.NewRequest(http.MethodPost, "https://example.test/inbox", strings.NewReader(tt.body))
			req.Host = tt.host
			for k, v := range headers {
				if strings.EqualFold(k, "host") {
					req.Host = v
					continue
				}
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			q := &fakeQueueClient{}
			NewHandler(cfg, nil, nil, q).ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestConcordeActivityPubResourceAcceptCases(t *testing.T) {
	cfg := testConfig()
	lookup := fakeActorLookup{actor: &actors.Actor{
		ID:           "alice-id",
		Username:     "alice",
		Name:         "Alice",
		Type:         "Person",
		URI:          "https://example.test/users/alice-id",
		Inbox:        "https://example.test/users/alice-id/inbox",
		SharedInbox:  "https://example.test/inbox",
		PublicKeyID:  "https://example.test/users/alice-id#main-key",
		PublicKeyPEM: "pem",
	}}
	for _, path := range []string{"/@alice", "/users/alice-id"} {
		for _, accept := range []string{"application/activity+json", "application/activity+json, */*"} {
			t.Run(path+" "+accept, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Accept", accept)
				rec := httptest.NewRecorder()
				NewHandler(cfg, nil, lookup, nil).ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
				}
				if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/activity+json") {
					t.Fatalf("Content-Type = %q", got)
				}
			})
		}
	}
}

func privateKeyPEM(key *rsa.PrivateKey) string {
	der := x509.MarshalPKCS1PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

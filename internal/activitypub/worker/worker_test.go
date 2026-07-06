package worker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeRepo struct {
	local  *actors.Actor
	remote *actors.Actor
}

func (f *fakeRepo) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == id {
		return f.local, nil
	}
	return nil, nil
}

func (f *fakeRepo) FindLocalByUsername(ctx context.Context, username string) (*actors.Actor, error) {
	if f.local != nil && f.local.Username == username {
		return f.local, nil
	}
	return nil, nil
}

func (f *fakeRepo) FindByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	if f.local != nil && f.local.URI == uri {
		return f.local, nil
	}
	if f.remote != nil && f.remote.URI == uri {
		return f.remote, nil
	}
	return nil, nil
}

func (f *fakeRepo) FindByPublicKeyID(ctx context.Context, keyID string) (*actors.Actor, error) {
	if f.remote != nil && f.remote.PublicKeyID == keyID {
		return f.remote, nil
	}
	return nil, nil
}

func (f *fakeRepo) UpsertRemoteActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	f.remote = &actor
	return f.remote, nil
}

type fakeQueue struct {
	task queue.Task
}

func (f *fakeQueue) Enqueue(ctx context.Context, task queue.Task) error {
	f.task = task
	return nil
}

type fakeClient struct{}

func (f *fakeClient) FetchObject(ctx context.Context, uri string, signer *actors.Actor) (map[string]any, error) {
	return nil, nil
}

func (f *fakeClient) Deliver(ctx context.Context, target string, signer actors.Actor, object map[string]any) error {
	return nil
}

func TestProcessInboxFollowEnqueuesAccept(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	signingString := "(request-target): post /inbox\nhost: rosmarinus.example"
	sum := sha256.Sum256([]byte(signingString))
	rawSig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15 returned error: %v", err)
	}
	host := "remote.example"
	local := &actors.Actor{
		ID:          "relay",
		Username:    "relay",
		URI:         "https://rosmarinus.example/users/relay",
		PublicKeyID: "https://rosmarinus.example/users/relay#main-key",
	}
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		SharedInbox:  "https://remote.example/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	q := &fakeQueue{}
	h := New(config.Config{
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, q, &fakeClient{}, local)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":     "https://remote.example/activities/follow",
			"type":   "Follow",
			"actor":  "https://remote.example/users/alice",
			"object": "https://rosmarinus.example/users/relay",
		},
		Signature: map[string]any{
			"keyId":         "https://remote.example/users/alice#main-key",
			"algorithm":     "rsa-sha256",
			"headers":       []string{"(request-target)", "host"},
			"signature":     base64.StdEncoding.EncodeToString(rawSig),
			"signingString": signingString,
		},
	})
	if err != nil {
		t.Fatalf("ProcessInbox returned error: %v", err)
	}
	if result != "ok: follow accepted delivery enqueued" {
		t.Fatalf("result = %q", result)
	}
	if q.task.Type != queue.TaskDeliver || q.task.Queue != queue.QueueDeliver {
		t.Fatalf("unexpected task: %+v", q.task)
	}
	payload, ok := q.task.Payload.(queue.DeliverPayload)
	if !ok {
		t.Fatalf("payload type = %T", q.task.Payload)
	}
	if payload.ActorID != "relay" || payload.To != "https://remote.example/inbox" {
		t.Fatalf("unexpected deliver payload: %+v", payload)
	}
	if payload.Object["type"] != "Accept" || payload.Object["actor"] != local.URI {
		t.Fatalf("unexpected accept activity: %+v", payload.Object)
	}
}

func publicKeyPEM(key *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

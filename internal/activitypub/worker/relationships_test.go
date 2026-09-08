package worker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestPerformUpdateRefreshesRemoteActorAndPreservesOmittedKey(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:  "relay",
		URI: "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID:           "remote-alice",
		Username:     "alice",
		Name:         "Old name",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: "old public key",
	}
	repo := &fakeRepo{local: local, remote: remote}
	h := New(config.Config{}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)

	result, err := h.performUpdate(context.Background(), remote, map[string]any{
		"type":  "Update",
		"actor": remote.URI,
		"object": map[string]any{
			"id":                remote.URI,
			"type":              "Person",
			"preferredUsername": "alice",
			"name":              "New name",
			"inbox":             "https://remote.example/users/alice/new-inbox",
			"outbox":            "https://remote.example/users/alice/outbox",
		},
	})
	if err != nil {
		t.Fatalf("performUpdate returned error: %v", err)
	}
	if result != "ok: Person updated" {
		t.Fatalf("result = %q", result)
	}
	if repo.remote == nil || repo.remote.Name != "New name" || repo.remote.Inbox != "https://remote.example/users/alice/new-inbox" {
		t.Fatalf("updated actor = %+v", repo.remote)
	}
	if repo.remote.PublicKeyID != remote.PublicKeyID || repo.remote.PublicKeyPEM != remote.PublicKeyPEM {
		t.Fatalf("omitted public key was not preserved: %+v", repo.remote)
	}
}

func TestPerformUpdateRejectsDifferentActorOnSameHost(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:  "relay",
		URI: "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID:       "remote-alice",
		Username: "alice",
		Name:     "Alice",
		Host:     &host,
		URI:      "https://remote.example/users/alice",
		Inbox:    "https://remote.example/users/alice/inbox",
	}
	repo := &fakeRepo{local: local, remote: remote}
	h := New(config.Config{}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)

	result, err := h.performUpdate(context.Background(), remote, map[string]any{
		"type":  "Update",
		"actor": remote.URI,
		"object": map[string]any{
			"id":                "https://remote.example/users/bob",
			"type":              "Person",
			"preferredUsername": "bob",
			"name":              "Bob",
			"inbox":             "https://remote.example/users/bob/inbox",
			"outbox":            "https://remote.example/users/bob/outbox",
		},
	})
	if err != nil {
		t.Fatalf("performUpdate returned error: %v", err)
	}
	if result != "skip: actor id mismatch" {
		t.Fatalf("result = %q", result)
	}
	if repo.remote != remote || repo.remote.Name != "Alice" {
		t.Fatalf("mismatched update changed actor: %+v", repo.remote)
	}
}

func TestProcessInboxRejectsAcceptWithoutIDLikeCurrentMisskey(t *testing.T) {
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
	local := &actors.Actor{ID: "local-alice", URI: "https://rosmarinus.example/users/local-alice"}
	remote := &actors.Actor{
		ID:           "remote-bob",
		URI:          "https://remote.example/users/bob",
		Host:         &host,
		PublicKeyID:  "https://remote.example/users/bob#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:       local.ID,
		FolloweeID:       remote.ID,
		FollowerURI:      local.URI,
		FolloweeURI:      remote.URI,
		Status:           follows.StatusPending,
		RemoteActivityID: "https://rosmarinus.example/follows/local-alice/remote-bob",
	})
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"type":  "Accept",
			"actor": remote.URI,
			"object": map[string]any{
				"id":     "https://rosmarinus.example/follows/local-alice/remote-bob",
				"type":   "Follow",
				"actor":  local.URI,
				"object": remote.URI,
			},
		},
		Signature: map[string]any{
			"keyId":         remote.PublicKeyID,
			"algorithm":     "rsa-sha256",
			"headers":       []string{"(request-target)", "host"},
			"signature":     base64.StdEncoding.EncodeToString(rawSig),
			"signingString": signingString,
		},
	})
	if err != nil || result != "skip: activity.id is not a string" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	follow, _ := followRepo.Find(context.Background(), local.ID, remote.ID)
	if follow == nil || follow.Status != follows.StatusPending {
		t.Fatalf("id-less accept changed follow state: %+v", follow)
	}
}

func TestRejectReferencedOutgoingFollowRemovesRequest(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local-alice", URI: "https://rosmarinus.example/users/local-alice"}
	remote := &actors.Actor{ID: "remote-bob", URI: "https://remote.example/users/bob", Host: &host}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:       local.ID,
		FolloweeID:       remote.ID,
		FollowerURI:      local.URI,
		FolloweeURI:      remote.URI,
		Status:           follows.StatusPending,
		RemoteActivityID: "https://rosmarinus.example/follows/local-alice/remote-bob",
	})
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	result, err := h.performRejectFollow(context.Background(), remote, map[string]any{
		"id":     "https://remote.example/rejects/1",
		"type":   "Reject",
		"object": "https://rosmarinus.example/follows/local-alice/remote-bob",
	})
	if err != nil || result != "ok: outgoing follow rejected" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if follow, _ := followRepo.Find(context.Background(), local.ID, remote.ID); follow != nil {
		t.Fatalf("follow was not removed: %+v", follow)
	}
}

func TestRejectFollowEnqueuesReject(t *testing.T) {
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
		PublicKeyPEM: "unused",
	}
	followsRepo := &fakeFollowRepo{}
	_, err := followsRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:          remote.ID,
		FolloweeID:          local.ID,
		FollowerURI:         remote.URI,
		FolloweeURI:         local.URI,
		FollowerHost:        remote.Host,
		FolloweeHost:        local.Host,
		FollowerInbox:       remote.Inbox,
		FollowerSharedInbox: remote.SharedInbox,
		Status:              follows.StatusPending,
		RemoteActivityID:    "https://remote.example/activities/follow",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	q := &fakeQueue{}
	connectorPublisher := &fakeConnectorPublisher{}
	h := New(config.Config{
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetConnectorPublisher(connectorPublisher)
	result, err := h.RejectFollow(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("RejectFollow returned error: %v", err)
	}
	if result != "ok: follow rejected delivery enqueued" {
		t.Fatalf("result = %q", result)
	}
	follow, err := followsRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if follow != nil {
		t.Fatalf("follow was not deleted: %+v", follow)
	}
	if q.task.Type != queue.TaskDeliver || q.task.Queue != queue.QueueDeliver {
		t.Fatalf("unexpected task: %+v", q.task)
	}
	payload, ok := q.task.Payload.(queue.DeliverPayload)
	if !ok {
		t.Fatalf("payload type = %T", q.task.Payload)
	}
	if payload.ActorID != "relay" || payload.To != "https://remote.example/users/alice/inbox" {
		t.Fatalf("unexpected deliver payload: %+v", payload)
	}
	if payload.Object["type"] != "Reject" || payload.Object["actor"] != local.URI {
		t.Fatalf("unexpected reject activity: %+v", payload.Object)
	}
	object, ok := payload.Object["object"].(map[string]any)
	if !ok {
		t.Fatalf("reject object type = %T", payload.Object["object"])
	}
	if object["id"] != "https://remote.example/activities/follow" || object["actor"] != remote.URI || object["object"] != local.URI {
		t.Fatalf("unexpected rejected follow object: %+v", object)
	}
	if connectorPublisher.rejected == nil {
		t.Fatalf("follow approval rejected event was not published")
	}
	if connectorPublisher.rejected.FollowerID != remote.ID || connectorPublisher.rejected.FolloweeID != local.ID {
		t.Fatalf("unexpected approval rejected payload: %+v", connectorPublisher.rejected)
	}
}

func TestRejectFollowSkipsAcceptedFollow(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:       "relay",
		Username: "relay",
		URI:      "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		SharedInbox:  "https://remote.example/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: "unused",
	}
	followsRepo := &fakeFollowRepo{}
	_, err := followsRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:          remote.ID,
		FolloweeID:          local.ID,
		FollowerURI:         remote.URI,
		FolloweeURI:         local.URI,
		FollowerHost:        remote.Host,
		FolloweeHost:        local.Host,
		FollowerInbox:       remote.Inbox,
		FollowerSharedInbox: remote.SharedInbox,
		Status:              follows.StatusAccepted,
		RemoteActivityID:    "https://remote.example/activities/follow",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	q := &fakeQueue{}
	h := New(config.Config{
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	result, err := h.RejectFollow(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("RejectFollow returned error: %v", err)
	}
	if result != "skip: follow request is not pending" {
		t.Fatalf("result = %q", result)
	}
	follow, err := followsRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if follow == nil || follow.Status != follows.StatusAccepted {
		t.Fatalf("accepted follow was changed: %+v", follow)
	}
	if q.task.Type != "" {
		t.Fatalf("unexpected task: %+v", q.task)
	}
}

func TestRejectAndBlockFollowDeliversBothActivities(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID: "local-alice", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/alice",
	}
	remote := &actors.Actor{
		ID: "remote-bob", URI: "https://remote.example/users/bob", Host: &host,
		Inbox: "https://remote.example/users/bob/inbox", SharedInbox: "https://remote.example/inbox",
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: remote.ID, FolloweeID: local.ID, FollowerURI: remote.URI, FolloweeURI: local.URI,
		FollowerHost: remote.Host, FollowerInbox: remote.Inbox, FollowerSharedInbox: remote.SharedInbox,
		Status: follows.StatusPending, RemoteActivityID: "https://remote.example/activities/follow",
	})
	blockRepo := &fakeBlockRepo{}
	q := &fakeQueue{}
	publisher := &fakeConnectorPublisher{}
	h := New(config.Config{
		PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetConnectorPublisher(publisher)

	created, err := h.RejectAndBlockFollow(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("RejectAndBlockFollow returned error: %v", err)
	}
	if created.BlockID != "block-id" || created.BlockeeID != remote.ID {
		t.Fatalf("created block = %+v", created)
	}
	if follow, _ := followRepo.Find(context.Background(), remote.ID, local.ID); follow != nil {
		t.Fatalf("pending follow remains: %+v", follow)
	}
	if block, _ := blockRepo.Find(context.Background(), local.ID, remote.ID); block == nil {
		t.Fatal("remote follower was not blocked")
	}
	if len(q.tasks) != 2 {
		t.Fatalf("delivery tasks = %d, want Reject and Block", len(q.tasks))
	}
	reject, rejectOK := q.tasks[0].Payload.(queue.DeliverPayload)
	block, blockOK := q.tasks[1].Payload.(queue.DeliverPayload)
	if !rejectOK || reject.Object["type"] != "Reject" || !blockOK || block.Object["type"] != "Block" {
		t.Fatalf("unexpected Reject and Block deliveries: %+v", q.tasks)
	}
	if publisher.rejected == nil || publisher.rejected.FollowerID != remote.ID {
		t.Fatalf("follow rejection event = %+v", publisher.rejected)
	}
}

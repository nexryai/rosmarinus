package worker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestProcessInboxFollowStoresPendingRequest(t *testing.T) {
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
		ID:             "relay",
		OwnerAccountID: "account-1",
		Username:       "relay",
		URI:            "https://rosmarinus.example/users/relay",
		PublicKeyID:    "https://rosmarinus.example/users/relay#main-key",
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
	connectorPublisher := &fakeConnectorPublisher{requestedErr: errors.New("publish failed")}
	followsRepo := &fakeFollowRepo{}
	h := New(config.Config{
		InboxQueue:              config.QueueConfig{Timeout: 5 * time.Minute},
		DeliverQueue:            config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
		InboxActivityReceiptTTL: 7 * 24 * time.Hour,
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	notificationRepo := &fakeNotificationRepo{}
	h.SetNotificationRepository(notificationRepo)
	locker := &fakeActivityLocker{acquired: true}
	h.SetActivityLocker(locker)
	h.SetConnectorPublisher(connectorPublisher)
	receipts := &fakeActivityReceiptRepo{}
	h.SetActivityReceiptRepository(receipts)
	payload := queue.InboxPayload{
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
	}
	if _, err := h.ProcessInbox(context.Background(), payload); err == nil {
		t.Fatalf("expected publisher failure")
	}
	if receipts.releases != 1 || len(receipts.active) != 0 {
		t.Fatalf("failed activity receipt was not released: %+v", receipts)
	}
	connectorPublisher.requestedErr = nil
	result, err := h.ProcessInbox(context.Background(), payload)
	if err != nil {
		t.Fatalf("ProcessInbox returned error: %v", err)
	}
	if result != "ok: follow request pending" {
		t.Fatalf("result = %q", result)
	}
	if q.task.Type != "" {
		t.Fatalf("unexpected task enqueued: %+v", q.task)
	}
	follow, err := followsRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if follow == nil || follow.FollowerURI != remote.URI || follow.FolloweeURI != local.URI {
		t.Fatalf("follow was not stored: %+v", follow)
	}
	if follow.Status != follows.StatusPending {
		t.Fatalf("follow status = %q", follow.Status)
	}
	if connectorPublisher.requested == nil {
		t.Fatalf("follow approval request event was not published")
	}
	if connectorPublisher.requested.FollowerID != remote.ID || connectorPublisher.requested.FolloweeID != local.ID {
		t.Fatalf("unexpected approval request payload: %+v", connectorPublisher.requested)
	}
	if len(notificationRepo.notifications) != 1 || connectorPublisher.notification == nil || connectorPublisher.notification.Kind != notifications.KindFollowRequest {
		t.Fatalf("follow notification was not persisted/published: stored=%+v event=%+v", notificationRepo.notifications, connectorPublisher.notification)
	}
	if !strings.HasPrefix(locker.name, "activity:") || !locker.unlocked {
		t.Fatalf("activity lock was not used and released: %+v", locker)
	}
	result, err = h.ProcessInbox(context.Background(), payload)
	if err != nil {
		t.Fatalf("duplicate ProcessInbox returned error: %v", err)
	}
	if result != "skip: activity was already processed or is in progress" {
		t.Fatalf("duplicate result = %q", result)
	}
	if receipts.claims != 3 || receipts.completes != 1 || receipts.releases != 1 {
		t.Fatalf("unexpected activity receipt transitions: %+v", receipts)
	}
	if receipts.lease != 6*time.Minute || receipts.retention != 7*24*time.Hour {
		t.Fatalf("unexpected receipt timings: lease=%s retention=%s", receipts.lease, receipts.retention)
	}
	if connectorPublisher.requestCalls != 2 {
		t.Fatalf("duplicate activity repeated connector side effects: calls=%d", connectorPublisher.requestCalls)
	}
}

func TestProcessInboxConcurrentDuplicatePerformsOneSideEffect(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	signingString := "(request-target): post /inbox\nhost: rosmarinus.example"
	sum := sha256.Sum256([]byte(signingString))
	rawSignature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15 returned error: %v", err)
	}
	host := "remote.example"
	local := &actors.Actor{
		ID: "relay", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID: "remote-alice", Host: &host, URI: "https://remote.example/users/alice",
		Inbox:       "https://remote.example/users/alice/inbox",
		PublicKeyID: "https://remote.example/users/alice#main-key", PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	followRepo := &fakeFollowRepo{}
	publisher := &fakeConnectorPublisher{}
	receipts := &fakeActivityReceiptRepo{}
	h := New(config.Config{
		InboxQueue: config.QueueConfig{Timeout: 5 * time.Minute}, InboxActivityReceiptTTL: 7 * 24 * time.Hour,
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetConnectorPublisher(publisher)
	h.SetActivityReceiptRepository(receipts)
	payload := queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id": "https://remote.example/activities/concurrent-follow", "type": "Follow",
			"actor": remote.URI, "object": local.URI,
		},
		Signature: map[string]any{
			"keyId": remote.PublicKeyID, "algorithm": "rsa-sha256",
			"headers":   []string{"(request-target)", "host"},
			"signature": base64.StdEncoding.EncodeToString(rawSignature), "signingString": signingString,
		},
	}

	const deliveries = 32
	start := make(chan struct{})
	results := make(chan string, deliveries)
	errors := make(chan error, deliveries)
	var workers sync.WaitGroup
	workers.Add(deliveries)
	for range deliveries {
		go func() {
			defer workers.Done()
			<-start
			result, err := h.ProcessInbox(context.Background(), payload)
			results <- result
			errors <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent ProcessInbox returned error: %v", err)
		}
	}
	performed := 0
	skipped := 0
	for result := range results {
		switch result {
		case "ok: follow request pending":
			performed++
		case "skip: activity was already processed or is in progress":
			skipped++
		default:
			t.Fatalf("unexpected concurrent result: %q", result)
		}
	}
	if performed != 1 || skipped != deliveries-1 {
		t.Fatalf("concurrent results performed=%d skipped=%d", performed, skipped)
	}
	if publisher.requestCalls != 1 {
		t.Fatalf("follow request side effects = %d", publisher.requestCalls)
	}
	if follow, findErr := followRepo.Find(context.Background(), remote.ID, local.ID); findErr != nil || follow == nil || follow.Status != follows.StatusPending {
		t.Fatalf("pending follow after duplicate delivery: follow=%+v err=%v", follow, findErr)
	}
	claims, completes, releases := receipts.counts()
	if claims != deliveries || completes != 1 || releases != 0 {
		t.Fatalf("receipt transitions claims=%d completes=%d releases=%d", claims, completes, releases)
	}
}

func TestProcessInboxRejectsBlockedSignatureHost(t *testing.T) {
	h := New(config.Config{FederationBlockedHosts: []string{"blocked.example"}}, nil, &fakeRepo{}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":    "https://social.blocked.example/activities/1",
			"type":  "Create",
			"actor": "https://social.blocked.example/users/alice",
		},
		Signature: map[string]any{
			"keyId":         "https://social.blocked.example/users/alice#main-key",
			"algorithm":     "rsa-sha256",
			"signature":     "AA==",
			"signingString": "(request-target): post /inbox",
		},
	})
	if err != nil || result != "skip: blocked request host=social.blocked.example" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestApproveFollowEnqueuesAccept(t *testing.T) {
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
	result, err := h.ApproveFollow(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("ApproveFollow returned error: %v", err)
	}
	if result != "ok: follow accepted delivery enqueued" {
		t.Fatalf("result = %q", result)
	}
	follow, err := followsRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if follow == nil || follow.Status != follows.StatusAccepted || follow.AcceptedAt == nil {
		t.Fatalf("follow was not accepted: %+v", follow)
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
	if payload.Object["type"] != "Accept" || payload.Object["actor"] != local.URI {
		t.Fatalf("unexpected accept activity: %+v", payload.Object)
	}
	object, ok := payload.Object["object"].(map[string]any)
	if !ok {
		t.Fatalf("accept object type = %T", payload.Object["object"])
	}
	if object["id"] != "https://remote.example/activities/follow" || object["actor"] != remote.URI || object["object"] != local.URI {
		t.Fatalf("unexpected accepted follow object: %+v", object)
	}
	if connectorPublisher.completed == nil {
		t.Fatalf("follow approval completed event was not published")
	}
	if connectorPublisher.completed.FollowerID != remote.ID || connectorPublisher.completed.FolloweeID != local.ID {
		t.Fatalf("unexpected approval completed payload: %+v", connectorPublisher.completed)
	}
}

func TestCreateFollowAndAcceptEstablishesOutgoingRelationship(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:          "local-alice",
		URI:         "https://rosmarinus.example/users/local-alice",
		PublicKeyID: "https://rosmarinus.example/users/local-alice#main-key",
	}
	remote := &actors.Actor{
		ID:          "remote-bob",
		URI:         "https://remote.example/users/bob",
		Host:        &host,
		Inbox:       "https://remote.example/users/bob/inbox",
		SharedInbox: "https://remote.example/inbox",
	}
	followRepo := &fakeFollowRepo{}
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)

	result, err := h.CreateFollow(context.Background(), local.ID, remote.URI)
	if err != nil {
		t.Fatalf("CreateFollow returned error: %v", err)
	}
	if result != "ok: follow delivery enqueued" {
		t.Fatalf("result = %q", result)
	}
	payload, ok := q.task.Payload.(queue.DeliverPayload)
	if !ok || payload.To != remote.Inbox || payload.Object["type"] != "Follow" {
		t.Fatalf("unexpected follow delivery: %+v", q.task)
	}
	follow, err := followRepo.Find(context.Background(), local.ID, remote.ID)
	if err != nil || follow == nil || follow.Status != follows.StatusPending {
		t.Fatalf("pending follow = %+v, err=%v", follow, err)
	}

	result, err = h.performAcceptFollow(context.Background(), remote, map[string]any{
		"id":    "https://remote.example/accepts/1",
		"type":  "Accept",
		"actor": remote.URI,
		"object": map[string]any{
			"id":     payload.Object["id"],
			"type":   "Follow",
			"actor":  local.URI,
			"object": remote.URI,
		},
	})
	if err != nil {
		t.Fatalf("performAcceptFollow returned error: %v", err)
	}
	if result != "ok: outgoing follow accepted" {
		t.Fatalf("result = %q", result)
	}
	follow, _ = followRepo.Find(context.Background(), local.ID, remote.ID)
	if follow == nil || follow.Status != follows.StatusAccepted || follow.AcceptedAt == nil {
		t.Fatalf("accepted follow = %+v", follow)
	}
}

func TestResolveRemoteActorAcceptsDisplayHandle(t *testing.T) {
	const actorURI = "https://remote.example/users/alice"
	repo := &fakeRepo{}
	client := &fakeClient{objects: map[string]map[string]any{
		actorURI: {
			"id": actorURI, "type": "Person", "preferredUsername": "alice",
			"inbox": actorURI + "/inbox",
		},
	}}
	webFinger := &fakeWebFinger{uri: actorURI}
	h := New(config.Config{}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	h.SetWebFingerResolver(webFinger)

	actor, err := h.ResolveRemoteActor(context.Background(), " @alice@remote.example ")
	if err != nil {
		t.Fatalf("ResolveRemoteActor returned error: %v", err)
	}
	if webFinger.query != "alice@remote.example" || actor == nil || actor.URI != actorURI || actor.Host == nil {
		t.Fatalf("query=%q actor=%+v", webFinger.query, actor)
	}
}

func TestAcceptResolvesReferencedOutgoingFollow(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local-alice", URI: "https://rosmarinus.example/users/local-alice"}
	remote := &actors.Actor{ID: "remote-bob", URI: "https://remote.example/users/bob", Host: &host}
	const followActivityID = "https://rosmarinus.example/follows/local-alice/remote-bob"
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: local.ID, FolloweeID: remote.ID,
		FollowerURI: local.URI, FolloweeURI: remote.URI,
		Status: follows.StatusPending, RemoteActivityID: followActivityID,
	})
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	result, err := h.performAcceptFollow(context.Background(), remote, map[string]any{
		"id": "https://remote.example/accepts/1", "type": "Accept",
		"actor": remote.URI, "object": followActivityID,
	})
	if err != nil || result != "ok: outgoing follow accepted" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	follow, _ := followRepo.Find(context.Background(), local.ID, remote.ID)
	if follow == nil || follow.Status != follows.StatusAccepted {
		t.Fatalf("referenced Follow was not accepted: %+v", follow)
	}
}

func TestDeleteFollowRemovesRelationshipAndEnqueuesUndo(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:  "local-alice",
		URI: "https://rosmarinus.example/users/local-alice",
	}
	remote := &actors.Actor{
		ID:          "remote-bob",
		URI:         "https://remote.example/users/bob",
		Host:        &host,
		Inbox:       "https://remote.example/users/bob/inbox",
		SharedInbox: "https://remote.example/inbox",
	}
	const followActivityID = "https://rosmarinus.example/follows/local-alice/remote-bob"
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:       local.ID,
		FolloweeID:       remote.ID,
		FollowerURI:      local.URI,
		FolloweeURI:      remote.URI,
		Status:           follows.StatusAccepted,
		RemoteActivityID: followActivityID,
	})
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)

	deleted, err := h.DeleteFollow(context.Background(), connector.FollowDeleteCommand{
		ActorID: local.ID,
		Target:  remote.URI,
	})
	if err != nil {
		t.Fatalf("DeleteFollow returned error: %v", err)
	}
	if deleted.FollowerID != local.ID || deleted.FolloweeID != remote.ID || deleted.URI != followActivityID+"/undo" {
		t.Fatalf("deleted follow = %+v", deleted)
	}
	if followRepo.deleted == nil || followRepo.deleted.FollowerID != local.ID || followRepo.deleted.FolloweeID != remote.ID {
		t.Fatalf("deleted relationship = %+v", followRepo.deleted)
	}
	if existing, _ := followRepo.Find(context.Background(), local.ID, remote.ID); existing != nil {
		t.Fatalf("follow relationship still exists: %+v", existing)
	}
	payload, ok := q.task.Payload.(queue.DeliverPayload)
	if !ok || payload.ActorID != local.ID || payload.To != remote.Inbox {
		t.Fatalf("unexpected Undo(Follow) delivery: %+v", q.task)
	}
	if payload.Object["id"] != followActivityID+"/undo" || payload.Object["type"] != "Undo" || payload.Object["actor"] != local.URI {
		t.Fatalf("unexpected Undo(Follow): %+v", payload.Object)
	}
	object, ok := payload.Object["object"].(map[string]any)
	if !ok || object["id"] != followActivityID || object["type"] != "Follow" || object["actor"] != local.URI || object["object"] != remote.URI {
		t.Fatalf("unexpected embedded Follow: %+v", payload.Object["object"])
	}
}

func TestCreateAndDeleteBlockRemovesFollowsAndDeliversActivities(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local-alice", URI: "https://rosmarinus.example/users/local-alice"}
	remote := &actors.Actor{
		ID: "remote-bob", URI: "https://remote.example/users/bob", Host: &host,
		Inbox: "https://remote.example/users/bob/inbox", SharedInbox: "https://remote.example/inbox",
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: local.ID, FolloweeID: remote.ID, FollowerURI: local.URI, FolloweeURI: remote.URI, Status: follows.StatusAccepted,
	})
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: remote.ID, FolloweeID: local.ID, FollowerURI: remote.URI, FolloweeURI: local.URI, Status: follows.StatusAccepted,
	})
	blockRepo := &fakeBlockRepo{}
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)

	created, err := h.CreateBlock(context.Background(), connector.BlockCreateCommand{ActorID: local.ID, Target: remote.URI})
	if err != nil {
		t.Fatalf("CreateBlock returned error: %v", err)
	}
	if created.BlockID != "block-id" || created.BlockeeID != remote.ID || created.URI != "https://rosmarinus.example/blocks/block-id" {
		t.Fatalf("created block = %+v", created)
	}
	if outgoing, _ := followRepo.Find(context.Background(), local.ID, remote.ID); outgoing != nil {
		t.Fatalf("outgoing follow remains: %+v", outgoing)
	}
	if incoming, _ := followRepo.Find(context.Background(), remote.ID, local.ID); incoming != nil {
		t.Fatalf("incoming follow remains: %+v", incoming)
	}
	blockPayload, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	if !ok || blockPayload.To != remote.Inbox || blockPayload.Object["type"] != "Block" || blockPayload.Object["actor"] != local.URI || blockPayload.Object["object"] != remote.URI {
		t.Fatalf("unexpected Block delivery: %+v", q.tasks[0])
	}

	deleted, err := h.DeleteBlock(context.Background(), connector.BlockDeleteCommand{ActorID: local.ID, Target: remote.URI})
	if err != nil {
		t.Fatalf("DeleteBlock returned error: %v", err)
	}
	if deleted.BlockID != "block-id" || deleted.BlockeeID != remote.ID || deleted.URI != "https://rosmarinus.example/blocks/block-id/undo" {
		t.Fatalf("deleted block = %+v", deleted)
	}
	if existing, _ := blockRepo.Find(context.Background(), local.ID, remote.ID); existing != nil {
		t.Fatalf("block remains: %+v", existing)
	}
	undoPayload, ok := q.tasks[1].Payload.(queue.DeliverPayload)
	if !ok || undoPayload.To != remote.Inbox || undoPayload.Object["type"] != "Undo" || undoPayload.Object["actor"] != local.URI {
		t.Fatalf("unexpected Undo(Block) delivery: %+v", q.tasks[1])
	}
	object, ok := undoPayload.Object["object"].(map[string]any)
	if !ok || object["id"] != "https://rosmarinus.example/blocks/block-id" || object["type"] != "Block" || object["object"] != remote.URI {
		t.Fatalf("unexpected embedded Block: %+v", undoPayload.Object["object"])
	}
}

func TestCreateAndDeleteBlockAppliesToEveryOwnedActor(t *testing.T) {
	host := "remote.example"
	first := &actors.Actor{ID: "actor-a", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/a"}
	second := &actors.Actor{ID: "actor-b", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/b"}
	suspended := &actors.Actor{ID: "actor-c", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/c", IsSuspended: true}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/remote", Host: &host, Inbox: "https://remote.example/users/remote/inbox"}
	followRepo := &fakeFollowRepo{}
	for _, local := range []*actors.Actor{first, second, suspended} {
		_, _ = followRepo.Upsert(context.Background(), follows.Follow{FollowerID: remote.ID, FolloweeID: local.ID, Status: follows.StatusAccepted})
	}
	blockRepo := &fakeBlockRepo{}
	queued := &fakeQueue{}
	repo := &fakeRepo{local: first, ownedLocals: []*actors.Actor{first, second, suspended}, remote: remote}
	h := New(config.Config{
		PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, repo, &fakeNoteRepo{}, followRepo, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, queued, &fakeClient{}, first)

	created, err := h.CreateBlock(context.Background(), connector.BlockCreateCommand{ActorID: first.ID, Target: remote.URI})
	if err != nil {
		t.Fatalf("CreateBlock returned error: %v", err)
	}
	if created.BlockeeID != remote.ID {
		t.Fatalf("created block = %+v", created)
	}
	for _, local := range []*actors.Actor{first, second, suspended} {
		if block, _ := blockRepo.Find(context.Background(), local.ID, remote.ID); block == nil {
			t.Fatalf("block missing for owned actor %s", local.ID)
		}
		if follow, _ := followRepo.Find(context.Background(), remote.ID, local.ID); follow != nil {
			t.Fatalf("incoming follow remains for owned actor %s: %+v", local.ID, follow)
		}
	}
	if len(queued.tasks) != 2 {
		t.Fatalf("Block deliveries = %d, want active owned actors only", len(queued.tasks))
	}
	for index, actorID := range []string{first.ID, second.ID} {
		payload, ok := queued.tasks[index].Payload.(queue.DeliverPayload)
		if !ok || payload.ActorID != actorID || payload.Object["type"] != "Block" {
			t.Fatalf("unexpected Block delivery %d: %+v", index, queued.tasks[index])
		}
	}

	deleted, err := h.DeleteBlock(context.Background(), connector.BlockDeleteCommand{ActorID: second.ID, Target: remote.URI})
	if err != nil {
		t.Fatalf("DeleteBlock returned error: %v", err)
	}
	if deleted.BlockeeID != remote.ID {
		t.Fatalf("deleted block = %+v", deleted)
	}
	for _, local := range []*actors.Actor{first, second, suspended} {
		if block, _ := blockRepo.Find(context.Background(), local.ID, remote.ID); block != nil {
			t.Fatalf("block remains for owned actor %s: %+v", local.ID, block)
		}
	}
	if len(queued.tasks) != 4 {
		t.Fatalf("total Block and Undo deliveries = %d, want four", len(queued.tasks))
	}
}

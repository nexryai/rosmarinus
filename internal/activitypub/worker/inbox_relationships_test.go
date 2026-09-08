package worker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestProcessInboxUndoFollowDeletesFollow(t *testing.T) {
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
	followsRepo := &fakeFollowRepo{}
	_, err = followsRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:  remote.ID,
		FolloweeID:  local.ID,
		FollowerURI: remote.URI,
		FolloweeURI: local.URI,
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":    "https://remote.example/activities/undo-follow",
			"type":  "Undo",
			"actor": "https://remote.example/users/alice",
			"object": map[string]any{
				"id":     "https://remote.example/activities/follow",
				"type":   "Follow",
				"actor":  "https://remote.example/users/alice",
				"object": "https://rosmarinus.example/users/relay",
			},
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
	if result != "ok: unfollowed" {
		t.Fatalf("result = %q", result)
	}
	follow, err := followsRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if follow != nil {
		t.Fatalf("follow still exists: %+v", follow)
	}
	if followsRepo.deleted == nil || followsRepo.deleted.RemoteUndoActivityID != "https://remote.example/activities/undo-follow" {
		t.Fatalf("delete was not recorded: %+v", followsRepo.deleted)
	}
}

func TestPerformUndoAcceptDeletesOutgoingFollow(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:  "relay",
		URI: "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID:   "remote-alice",
		URI:  "https://remote.example/users/alice",
		Host: &host,
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:  local.ID,
		FolloweeID:  remote.ID,
		FollowerURI: local.URI,
		FolloweeURI: remote.URI,
		Status:      follows.StatusAccepted,
	})
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)

	result, err := h.performUndo(context.Background(), remote, map[string]any{
		"id":    "https://remote.example/activities/undo-accept",
		"type":  "Undo",
		"actor": remote.URI,
		"object": map[string]any{
			"id":    "https://remote.example/activities/accept",
			"type":  "Accept",
			"actor": remote.URI,
			"object": map[string]any{
				"id":     "https://rosmarinus.example/follows/relay/remote-alice",
				"type":   "Follow",
				"actor":  local.URI,
				"object": remote.URI,
			},
		},
	})
	if err != nil {
		t.Fatalf("performUndo returned error: %v", err)
	}
	if result != "ok: unfollowed" {
		t.Fatalf("result = %q", result)
	}
	if follow, _ := followRepo.Find(context.Background(), local.ID, remote.ID); follow != nil {
		t.Fatalf("follow still exists: %+v", follow)
	}
	if followRepo.deleted == nil || followRepo.deleted.RemoteUndoActivityID != "https://remote.example/activities/undo-accept" {
		t.Fatalf("delete was not recorded: %+v", followRepo.deleted)
	}
}

func TestPerformUndoAcceptRejectsMismatchedFollowee(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID:  "relay",
		URI: "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID:   "remote-alice",
		URI:  "https://remote.example/users/alice",
		Host: &host,
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:  local.ID,
		FolloweeID:  remote.ID,
		FollowerURI: local.URI,
		FolloweeURI: remote.URI,
		Status:      follows.StatusAccepted,
	})
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)

	result, err := h.performUndo(context.Background(), remote, map[string]any{
		"id":    "https://remote.example/activities/undo-accept",
		"type":  "Undo",
		"actor": remote.URI,
		"object": map[string]any{
			"type":  "Accept",
			"actor": remote.URI,
			"object": map[string]any{
				"type":   "Follow",
				"actor":  local.URI,
				"object": "https://other.example/users/mallory",
			},
		},
	})
	if err != nil {
		t.Fatalf("performUndo returned error: %v", err)
	}
	if result != "skip: accepted follow object mismatch" {
		t.Fatalf("result = %q", result)
	}
	if follow, _ := followRepo.Find(context.Background(), local.ID, remote.ID); follow == nil {
		t.Fatal("mismatched Undo(Accept) deleted the follow")
	}
}

func TestProcessInboxBlockStoresBlock(t *testing.T) {
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
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	blockRepo := &fakeBlockRepo{}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{FollowerID: remote.ID, FolloweeID: local.ID, Status: follows.StatusAccepted})
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{FollowerID: local.ID, FolloweeID: remote.ID, Status: follows.StatusAccepted})
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":     "https://remote.example/activities/block",
			"type":   "Block",
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
	if result != "ok" {
		t.Fatalf("result = %q", result)
	}
	block, err := blockRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if block == nil || block.BlockerURI != remote.URI || block.BlockeeURI != local.URI {
		t.Fatalf("block was not stored: %+v", block)
	}
	for _, pair := range [][2]string{{remote.ID, local.ID}, {local.ID, remote.ID}} {
		if follow, _ := followRepo.Find(context.Background(), pair[0], pair[1]); follow != nil {
			t.Fatalf("blocked follow was retained: %+v", follow)
		}
	}
}

func TestProcessInboxUndoBlockDeletesBlock(t *testing.T) {
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
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	blockRepo := &fakeBlockRepo{}
	_, err = blockRepo.Upsert(context.Background(), blocks.Block{
		BlockerID:  remote.ID,
		BlockeeID:  local.ID,
		BlockerURI: remote.URI,
		BlockeeURI: local.URI,
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{
		objects: map[string]map[string]any{
			"https://remote.example/activities/block": {
				"id":     "https://remote.example/activities/block",
				"type":   "Block",
				"actor":  "https://remote.example/users/alice",
				"object": "https://rosmarinus.example/users/relay",
			},
		},
	}, local)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":     "https://remote.example/activities/undo-block",
			"type":   "Undo",
			"actor":  "https://remote.example/users/alice",
			"object": "https://remote.example/activities/block",
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
	if result != "ok" {
		t.Fatalf("result = %q", result)
	}
	block, err := blockRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if block != nil {
		t.Fatalf("block still exists: %+v", block)
	}
	if blockRepo.deleted == nil || blockRepo.deleted.RemoteUndoActivityID != "https://remote.example/activities/undo-block" {
		t.Fatalf("delete was not recorded: %+v", blockRepo.deleted)
	}
}

func TestProcessInboxFlagStoresReportForLocalUser(t *testing.T) {
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
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	reportRepo := &fakeReportRepo{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, reportRepo, &fakeQueue{}, &fakeClient{}, local)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":      "https://remote.example/activities/flag",
			"type":    "Flag",
			"actor":   "https://remote.example/users/alice",
			"content": "spam report",
			"object": []any{
				"https://remote.example/notes/1",
				"https://rosmarinus.example/users/relay",
			},
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
	if result != "ok" {
		t.Fatalf("result = %q", result)
	}
	report, err := reportRepo.FindByRemoteActivityID(context.Background(), "https://remote.example/activities/flag")
	if err != nil {
		t.Fatalf("FindByRemoteActivityID returned error: %v", err)
	}
	if report == nil {
		t.Fatalf("report was not stored")
	}
	if report.TargetUserID != local.ID || report.ReporterID != remote.ID || report.ReporterURI != remote.URI {
		t.Fatalf("unexpected report identity: %+v", report)
	}
	if len(report.ObjectURIs) != 2 || report.ObjectURIs[1] != local.URI {
		t.Fatalf("object uris = %#v", report.ObjectURIs)
	}
	if report.Comment == "" || report.Content != "spam report" {
		t.Fatalf("unexpected report content: %+v", report)
	}
}

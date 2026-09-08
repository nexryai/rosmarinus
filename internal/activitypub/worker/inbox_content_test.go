package worker

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/cleanup"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestProcessInboxCreateStoresNote(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	noteRepo := &fakeNoteRepo{}
	client := &fakeClient{objects: map[string]map[string]any{
		"https://remote.example/notes/root": {
			"id": "https://remote.example/notes/root", "type": "Note",
			"attributedTo": remote.URI, "to": apnotes.PublicAudience, "content": "root",
		},
		"https://remote.example/notes/quote": {
			"id": "https://remote.example/notes/quote", "type": "Note",
			"attributedTo": remote.URI, "to": apnotes.PublicAudience, "content": "quote",
		},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":    "https://remote.example/activities/create",
			"type":  "Create",
			"actor": "https://remote.example/users/alice",
			"to":    "https://www.w3.org/ns/activitystreams#Public",
			"object": map[string]any{
				"id":             "https://remote.example/notes/1",
				"type":           "Note",
				"content":        "hello",
				"summary":        "cw",
				"sensitive":      true,
				"inReplyTo":      "https://remote.example/notes/root",
				"_misskey_quote": "acct:permanently-invalid",
				"quoteUrl":       "https://remote.example/notes/quote",
				"tag": []any{
					map[string]any{"type": "Hashtag", "name": "#hello"},
				},
				"attachment": []any{
					map[string]any{
						"type":      "Document",
						"mediaType": "image/png",
						"url":       "https://remote.example/files/1.png",
						"name":      "file",
					},
				},
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
	if result != "ok: note created" {
		t.Fatalf("result = %q", result)
	}
	note := noteRepo.notes["https://remote.example/notes/1"]
	if note == nil {
		t.Fatalf("note was not stored")
	}
	if note.AttributedTo != remote.URI || note.Text != "hello" || note.Visibility != domainnotes.VisibilityPublic {
		t.Fatalf("unexpected note: %+v", note)
	}
	if note.ContentWarning == nil || *note.ContentWarning != "cw" || !note.Sensitive {
		t.Fatalf("unexpected cw/sensitive: %+v", note)
	}
	if note.InReplyToURI != "https://remote.example/notes/root" || note.QuoteURI != "https://remote.example/notes/quote" {
		t.Fatalf("unexpected reply/quote: %+v", note)
	}
	if note.ReplyID == "" || note.QuoteID == "" {
		t.Fatalf("reply/quote were not resolved: %+v", note)
	}
	if len(note.Hashtags) != 1 || note.Hashtags[0] != "hello" {
		t.Fatalf("hashtags = %#v", note.Hashtags)
	}
	if len(note.Attachments) != 1 || note.Attachments[0].URL != "https://remote.example/files/1.png" {
		t.Fatalf("attachments = %#v", note.Attachments)
	}
}

func TestProcessInboxDeleteRemovesNote(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"https://remote.example/notes/1": {
			ID:           "note-id",
			URI:          "https://remote.example/notes/1",
			AttributedTo: remote.URI,
			AuthorID:     remote.ID,
			Text:         "hello",
			Visibility:   domainnotes.VisibilityPublic,
		},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	cleanupRepo := &fakeAccountCleanupRepo{}
	h.SetAccountCleanupRepository(cleanupRepo)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":    "https://remote.example/activities/delete",
			"type":  "Delete",
			"actor": "https://remote.example/users/alice",
			"object": map[string]any{
				"id":         "https://remote.example/notes/1",
				"type":       "Tombstone",
				"formerType": "Note",
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
	if result != "ok: note deleted" {
		t.Fatalf("result = %q", result)
	}
	if noteRepo.notes["https://remote.example/notes/1"] != nil {
		t.Fatalf("note still exists: %+v", noteRepo.notes["https://remote.example/notes/1"])
	}
	if cleanupRepo.noteID != "note-id" {
		t.Fatalf("note dependencies were not cleaned: %+v", cleanupRepo)
	}
}

func TestProcessInboxDeleteActorQueuesAccountDelete(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	repo := &fakeRepo{remote: remote}
	q := &fakeQueue{}
	h := New(config.Config{}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, nil)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":    "https://remote.example/activities/delete-account",
			"type":  "Delete",
			"actor": "https://remote.example/users/alice",
			"object": map[string]any{
				"id":         "https://remote.example/users/alice",
				"type":       "Tombstone",
				"formerType": "Person",
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
	if result != "ok: account delete queued" {
		t.Fatalf("result = %q", result)
	}
	if repo.deletedRemoteURI != remote.URI {
		t.Fatalf("deletedRemoteURI = %q", repo.deletedRemoteURI)
	}
	if !remote.IsSuspended {
		t.Fatalf("remote actor was not suspended")
	}
	if q.task.Type != queue.TaskAccountDelete || q.task.Queue != queue.QueueAccountDelete {
		t.Fatalf("unexpected task: %+v", q.task)
	}
	payload, ok := q.task.Payload.(queue.AccountDeletePayload)
	if !ok {
		t.Fatalf("payload type = %T", q.task.Payload)
	}
	if payload.Version != 1 || payload.ActorID != remote.ID || payload.ActorURI != remote.URI {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandleAccountDeleteTaskCleansSuspendedRemoteActor(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{
		ID: "remote_alice", URI: "https://remote.example/users/alice", Host: &host, IsSuspended: true,
	}
	deletedAt := time.Now().UTC()
	remote.DeletedAt = &deletedAt
	cleanupRepo := &fakeAccountCleanupRepo{result: cleanup.Result{Notes: 2, Follows: 1}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	h.SetAccountCleanupRepository(cleanupRepo)
	payload, err := json.Marshal(queue.AccountDeletePayload{Version: 1, ActorID: remote.ID, ActorURI: remote.URI})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := h.HandleAccountDeleteTask(context.Background(), asynq.NewTask(queue.TaskAccountDelete, payload)); err != nil {
		t.Fatalf("HandleAccountDeleteTask returned error: %v", err)
	}
	if cleanupRepo.actorID != remote.ID {
		t.Fatalf("cleanup actor id = %q", cleanupRepo.actorID)
	}
}

func TestHandleAccountDeleteTaskCleansDeletedLocalActor(t *testing.T) {
	deletedAt := time.Now().UTC()
	local := &actors.Actor{
		ID: "owned-alice", URI: "https://rosmarinus.example/users/owned-alice",
		OwnerAccountID: "account-1", IsSuspended: true, DeletedAt: &deletedAt,
	}
	cleanupRepo := &fakeAccountCleanupRepo{result: cleanup.Result{Notes: 2, Follows: 1}}
	h := New(config.Config{}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	h.SetAccountCleanupRepository(cleanupRepo)
	payload, err := json.Marshal(queue.AccountDeletePayload{Version: 1, ActorID: local.ID, ActorURI: local.URI, Local: true})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if err := h.HandleAccountDeleteTask(context.Background(), asynq.NewTask(queue.TaskAccountDelete, payload)); err != nil {
		t.Fatalf("HandleAccountDeleteTask returned error: %v", err)
	}
	if cleanupRepo.actorID != local.ID {
		t.Fatalf("cleanup actor id = %q", cleanupRepo.actorID)
	}
}

func TestHandleAccountDeleteTaskRejectsActiveActor(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote_alice", URI: "https://remote.example/users/alice", Host: &host}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	h.SetAccountCleanupRepository(&fakeAccountCleanupRepo{})
	payload, _ := json.Marshal(queue.AccountDeletePayload{Version: 1, ActorID: remote.ID, ActorURI: remote.URI})
	if err := h.HandleAccountDeleteTask(context.Background(), asynq.NewTask(queue.TaskAccountDelete, payload)); err == nil {
		t.Fatal("active remote actor cleanup was accepted")
	}
}

func TestHandlePollEndedTaskNotifiesLocalOwner(t *testing.T) {
	owner := &actors.Actor{ID: "local", URI: "https://local.example/users/alice", OwnerAccountID: "account-1"}
	note := &domainnotes.Note{ID: "poll-note", URI: "https://local.example/notes/poll", AuthorID: owner.ID, AttributedTo: owner.URI, Visibility: domainnotes.VisibilityPublic}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{note.ID: note, note.URI: note}}
	expiresAt := time.Now().UTC().Add(-time.Minute)
	pollRepo := &fakePollRepo{polls: map[string]*domainpolls.Poll{
		note.ID: {NoteID: note.ID, AuthorID: owner.ID, Choices: []string{"cats", "dogs"}, Votes: []int{1, 0}, ExpiresAt: &expiresAt},
	}}
	notificationRepo := &fakeNotificationRepo{}
	h := New(config.Config{}, nil, &fakeRepo{local: owner}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, owner)
	h.SetPollRepository(pollRepo)
	h.SetNotificationRepository(notificationRepo)
	payload, _ := json.Marshal(queue.PollEndedPayload{Version: 1, NoteID: note.ID})
	if err := h.HandlePollEndedTask(context.Background(), asynq.NewTask(queue.TaskPollEnded, payload)); err != nil {
		t.Fatalf("HandlePollEndedTask returned error: %v", err)
	}
	if len(notificationRepo.notifications) != 1 {
		t.Fatalf("notifications = %#v", notificationRepo.notifications)
	}
	for _, notification := range notificationRepo.notifications {
		if notification.Kind != notifications.KindPollEnded || notification.NoteID != note.ID || notification.RecipientActorID != owner.ID {
			t.Fatalf("unexpected notification: %+v", notification)
		}
	}
}

func TestProcessInboxLikeStoresReaction(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	local := &actors.Actor{ID: "relay", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/relay"}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"https://rosmarinus.example/notes/1": {
			ID:           "note-id",
			URI:          "https://rosmarinus.example/notes/1",
			AttributedTo: "https://rosmarinus.example/users/relay",
			AuthorID:     "relay",
			Text:         "hello",
			Visibility:   domainnotes.VisibilityPublic,
		},
	}}
	reactionRepo := &fakeReactionRepo{}
	emojiRepo := &fakeEmojiRepo{}
	notificationRepo := &fakeNotificationRepo{}
	connectorPublisher := &fakeConnectorPublisher{}
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, reactionRepo, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	h.SetNotificationRepository(notificationRepo)
	h.SetEmojiRepository(emojiRepo)
	h.SetConnectorPublisher(connectorPublisher)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":                "https://remote.example/activities/like",
			"type":              "EmojiReaction",
			"actor":             "https://remote.example/users/alice",
			"object":            "https://rosmarinus.example/notes/1",
			"_misskey_reaction": ":party@example.com:",
			"content":           "ignored",
			"name":              "also-ignored",
			"tag": []any{map[string]any{
				"id": "https://remote.example/emojis/party", "type": "Emoji", "name": ":party:",
				"updated": "2026-08-24T00:00:00Z",
				"icon":    map[string]any{"url": "https://remote.example/files/party.webp", "mediaType": "image/webp"},
			}},
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
	if result != "ok: reaction created" {
		t.Fatalf("result = %q", result)
	}
	reaction, err := reactionRepo.Find(context.Background(), "note-id", remote.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if reaction == nil {
		t.Fatalf("reaction was not stored")
	}
	if reaction.NoteURI != "https://rosmarinus.example/notes/1" || reaction.ActorURI != remote.URI {
		t.Fatalf("unexpected reaction identity: %+v", reaction)
	}
	if reaction.Reaction != ":party@example.com:" || reaction.RemoteActivityID != "https://remote.example/activities/like" {
		t.Fatalf("unexpected reaction payload: %+v", reaction)
	}
	storedEmoji := emojiRepo.emojis["remote.example\x00party"]
	if storedEmoji == nil || storedEmoji.URI != "https://remote.example/emojis/party" || storedEmoji.OriginalURL != "https://remote.example/files/party.webp" {
		t.Fatalf("reaction emoji was not upserted: %+v", storedEmoji)
	}
	if len(notificationRepo.notifications) != 1 || connectorPublisher.notification == nil || connectorPublisher.notification.Kind != notifications.KindReaction {
		t.Fatalf("reaction notification was not persisted/published: stored=%+v event=%+v", notificationRepo.notifications, connectorPublisher.notification)
	}
}

func TestProcessInboxAnnounceStoresRenote(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/local"}
	noteRepo := &fakeNoteRepo{}
	publisher := &fakeConnectorPublisher{}
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{
		objects: map[string]map[string]any{
			"https://remote.example/notes/1": {
				"id":           "https://remote.example/notes/1",
				"type":         "Note",
				"attributedTo": "https://remote.example/users/alice",
				"to":           "https://www.w3.org/ns/activitystreams#Public",
				"content":      "hello",
			},
		},
	}, nil)
	h.SetConnectorPublisher(publisher)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":        "https://remote.example/notes/renote/activity",
			"type":      "Announce",
			"actor":     "https://remote.example/users/alice",
			"object":    "https://remote.example/notes/1",
			"published": "2026-07-08T00:00:00Z",
			"to":        "https://www.w3.org/ns/activitystreams#Public",
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
	if result != "ok: announce created" {
		t.Fatalf("result = %q", result)
	}
	target := noteRepo.notes["https://remote.example/notes/1"]
	if target == nil {
		t.Fatalf("announce target was not stored")
	}
	announce := noteRepo.notes["https://remote.example/notes/renote/activity"]
	if announce == nil {
		t.Fatalf("announce was not stored")
	}
	if announce.AuthorID != remote.ID || announce.AttributedTo != remote.URI {
		t.Fatalf("unexpected announce actor: %+v", announce)
	}
	if announce.RenoteURI != target.URI || announce.RenoteID != target.ID {
		t.Fatalf("unexpected renote reference: %+v target=%+v", announce, target)
	}
	if announce.Visibility != domainnotes.VisibilityPublic {
		t.Fatalf("visibility = %q", announce.Visibility)
	}
	if len(publisher.posts) != 1 || publisher.posts[0].AccountID != local.OwnerAccountID || publisher.posts[0].NoteID != announce.ID {
		t.Fatalf("announce note.created event = %+v", publisher.posts)
	}
}

func TestPerformAnnounceRejectsUnshareableTargetVisibility(t *testing.T) {
	host := "remote.example"
	actor := &actors.Actor{ID: "remote-bob", URI: "https://remote.example/users/bob", Host: &host}
	targetAuthor := &actors.Actor{ID: "remote-alice", URI: "https://remote.example/users/alice", Host: &host}
	for _, visibility := range []domainnotes.Visibility{domainnotes.VisibilityFollowers, domainnotes.VisibilitySpecified} {
		t.Run(string(visibility), func(t *testing.T) {
			targetURI := "https://remote.example/notes/target-" + string(visibility)
			noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
				targetURI: {
					ID: "target-" + string(visibility), URI: targetURI,
					AuthorID: targetAuthor.ID, AttributedTo: targetAuthor.URI,
					Visibility: visibility,
				},
			}}
			h := New(config.Config{}, nil, &fakeRepo{remote: actor}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
			result, err := h.performAnnounce(context.Background(), actor, map[string]any{
				"id":   "https://remote.example/activities/announce-" + string(visibility),
				"type": "Announce", "actor": actor.URI, "object": targetURI,
				"to": apnotes.PublicAudience,
			})
			if err != nil || result != "skip: announce target is not shareable" {
				t.Fatalf("result=%q err=%v", result, err)
			}
			if len(noteRepo.notes) != 1 {
				t.Fatalf("unshareable Announce was stored: %#v", noteRepo.notes)
			}
		})
	}
}

func TestPerformAnnounceRejectsCachedTargetFromBlockedHost(t *testing.T) {
	host := "remote.example"
	actor := &actors.Actor{ID: "remote-bob", URI: "https://remote.example/users/bob", Host: &host}
	targetURI := "https://social.blocked.example/notes/target"
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		targetURI: {
			ID: "target", URI: targetURI, AuthorID: "remote-alice",
			AttributedTo: "https://social.blocked.example/users/alice",
			Visibility:   domainnotes.VisibilityPublic,
		},
	}}
	h := New(config.Config{FederationBlockedHosts: []string{"blocked.example"}}, nil, &fakeRepo{remote: actor}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	result, err := h.performAnnounce(context.Background(), actor, map[string]any{
		"id": "https://remote.example/activities/announce", "type": "Announce",
		"actor": actor.URI, "object": targetURI, "to": apnotes.PublicAudience,
	})
	if err != nil || result != "skip: announce target host is blocked" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(noteRepo.notes) != 1 {
		t.Fatalf("blocked Announce was stored: %#v", noteRepo.notes)
	}
}

func TestProcessInboxUndoLikeDeletesReaction(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"https://rosmarinus.example/notes/1": {
			ID:           "note-id",
			URI:          "https://rosmarinus.example/notes/1",
			AttributedTo: "https://rosmarinus.example/users/relay",
			AuthorID:     "relay",
			Text:         "hello",
			Visibility:   domainnotes.VisibilityPublic,
		},
	}}
	reactionRepo := &fakeReactionRepo{}
	_, err = reactionRepo.Upsert(context.Background(), reactions.Reaction{
		NoteID:   "note-id",
		NoteURI:  "https://rosmarinus.example/notes/1",
		ActorID:  remote.ID,
		ActorURI: remote.URI,
		Reaction: ":party@example.com:",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, reactionRepo, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{
		objects: map[string]map[string]any{
			"https://remote.example/activities/like": {
				"id":                "https://remote.example/activities/like",
				"type":              "Like",
				"actor":             "https://remote.example/users/alice",
				"object":            "https://rosmarinus.example/notes/1",
				"_misskey_reaction": ":party@example.com:",
			},
		},
	}, nil)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":     "https://remote.example/activities/undo-like",
			"type":   "Undo",
			"actor":  "https://remote.example/users/alice",
			"object": "https://remote.example/activities/like",
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
	if result != "ok: reaction deleted" {
		t.Fatalf("result = %q", result)
	}
	reaction, err := reactionRepo.Find(context.Background(), "note-id", remote.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if reaction != nil {
		t.Fatalf("reaction still exists: %+v", reaction)
	}
	if reactionRepo.deleted == nil || reactionRepo.deleted.RemoteUndoActivityID != "https://remote.example/activities/undo-like" {
		t.Fatalf("delete was not recorded: %+v", reactionRepo.deleted)
	}
}

func TestProcessInboxUndoAnnounceDeletesRenote(t *testing.T) {
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
	remote := &actors.Actor{
		ID:           "remote_alice",
		Username:     "alice",
		Host:         &host,
		URI:          "https://remote.example/users/alice",
		Inbox:        "https://remote.example/users/alice/inbox",
		PublicKeyID:  "https://remote.example/users/alice#main-key",
		PublicKeyPEM: publicKeyPEM(&privateKey.PublicKey),
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"https://remote.example/activities/announce": {
			ID:           "announce-id",
			URI:          "https://remote.example/activities/announce",
			AttributedTo: remote.URI,
			AuthorID:     remote.ID,
			RenoteID:     "note-id",
			RenoteURI:    "https://rosmarinus.example/notes/1",
			Visibility:   domainnotes.VisibilityPublic,
		},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{
		objects: map[string]map[string]any{
			"https://remote.example/activities/announce": {
				"id":     "https://remote.example/activities/announce",
				"type":   "Announce",
				"actor":  "https://remote.example/users/alice",
				"object": "https://rosmarinus.example/notes/1",
			},
		},
	}, nil)
	result, err := h.ProcessInbox(context.Background(), queue.InboxPayload{
		Version: 1,
		Activity: map[string]any{
			"id":     "https://remote.example/activities/undo-announce",
			"type":   "Undo",
			"actor":  "https://remote.example/users/alice",
			"object": "https://remote.example/activities/announce",
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
	if result != "ok: deleted" {
		t.Fatalf("result = %q", result)
	}
	if noteRepo.notes["https://remote.example/activities/announce"] != nil {
		t.Fatalf("announce still exists: %+v", noteRepo.notes["https://remote.example/activities/announce"])
	}
}

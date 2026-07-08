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
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
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

type fakeFollowRepo struct {
	follows map[string]*follows.Follow
	deleted *follows.Follow
}

func (f *fakeFollowRepo) Find(ctx context.Context, followerID, followeeID string) (*follows.Follow, error) {
	if f.follows == nil {
		return nil, nil
	}
	return f.follows[followerID+"\x00"+followeeID], nil
}

func (f *fakeFollowRepo) Upsert(ctx context.Context, follow follows.Follow) (*follows.Follow, error) {
	if f.follows == nil {
		f.follows = map[string]*follows.Follow{}
	}
	key := follow.FollowerID + "\x00" + follow.FolloweeID
	if existing := f.follows[key]; existing != nil {
		return existing, nil
	}
	if follow.ID == "" {
		follow.ID = "follow-id"
	}
	f.follows[key] = &follow
	return &follow, nil
}

func (f *fakeFollowRepo) Delete(ctx context.Context, followerID, followeeID, remoteUndoActivityID string) error {
	existing, _ := f.Find(ctx, followerID, followeeID)
	if existing != nil {
		copy := *existing
		copy.RemoteUndoActivityID = remoteUndoActivityID
		f.deleted = &copy
		delete(f.follows, followerID+"\x00"+followeeID)
	}
	return nil
}

type fakeNoteRepo struct {
	notes map[string]*domainnotes.Note
}

func (f *fakeNoteRepo) FindByID(ctx context.Context, id string) (*domainnotes.Note, error) {
	for _, note := range f.notes {
		if note.ID == id {
			return note, nil
		}
	}
	return nil, nil
}

func (f *fakeNoteRepo) FindByURI(ctx context.Context, uri string) (*domainnotes.Note, error) {
	if f.notes == nil {
		return nil, nil
	}
	return f.notes[uri], nil
}

func (f *fakeNoteRepo) UpsertRemoteNote(ctx context.Context, note domainnotes.Note) (*domainnotes.Note, error) {
	if f.notes == nil {
		f.notes = map[string]*domainnotes.Note{}
	}
	if existing := f.notes[note.URI]; existing != nil {
		return existing, nil
	}
	if note.ID == "" {
		note.ID = "note-id"
	}
	f.notes[note.URI] = &note
	return &note, nil
}

func (f *fakeNoteRepo) DeleteRemoteNote(ctx context.Context, uri, authorID string) error {
	if f.notes == nil {
		return nil
	}
	note := f.notes[uri]
	if note == nil || note.AuthorID != authorID {
		return nil
	}
	now := time.Now().UTC()
	note.DeletedAt = &now
	delete(f.notes, uri)
	return nil
}

type fakeReactionRepo struct {
	reactions map[string]*reactions.Reaction
	deleted   *reactions.Reaction
}

func (f *fakeReactionRepo) Find(ctx context.Context, noteID, actorID string) (*reactions.Reaction, error) {
	if f.reactions == nil {
		return nil, nil
	}
	return f.reactions[noteID+"\x00"+actorID], nil
}

func (f *fakeReactionRepo) Upsert(ctx context.Context, reaction reactions.Reaction) (*reactions.Reaction, error) {
	if f.reactions == nil {
		f.reactions = map[string]*reactions.Reaction{}
	}
	key := reaction.NoteID + "\x00" + reaction.ActorID
	if reaction.ID == "" {
		reaction.ID = "reaction-id"
	}
	f.reactions[key] = &reaction
	return &reaction, nil
}

func (f *fakeReactionRepo) Delete(ctx context.Context, noteID, actorID, remoteUndoActivityID string) error {
	existing, _ := f.Find(ctx, noteID, actorID)
	if existing != nil {
		copy := *existing
		copy.RemoteUndoActivityID = remoteUndoActivityID
		f.deleted = &copy
		delete(f.reactions, noteID+"\x00"+actorID)
	}
	return nil
}

type fakeClient struct {
	objects map[string]map[string]any
}

func (f *fakeClient) FetchObject(ctx context.Context, uri string, signer *actors.Actor) (map[string]any, error) {
	return f.objects[uri], nil
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
	followsRepo := &fakeFollowRepo{}
	h := New(config.Config{
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeReactionRepo{}, q, &fakeClient{}, local)
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
	follow, err := followsRepo.Find(context.Background(), remote.ID, local.ID)
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if follow == nil || follow.FollowerURI != remote.URI || follow.FolloweeURI != local.URI {
		t.Fatalf("follow was not stored: %+v", follow)
	}
}

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
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeReactionRepo{}, &fakeQueue{}, &fakeClient{}, local)
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeReactionRepo{}, &fakeQueue{}, &fakeClient{}, nil)
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
				"_misskey_quote": "https://remote.example/notes/quote",
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeReactionRepo{}, &fakeQueue{}, &fakeClient{}, nil)
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, reactionRepo, &fakeQueue{}, &fakeClient{}, nil)
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, reactionRepo, &fakeQueue{}, &fakeClient{
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

func publicKeyPEM(key *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

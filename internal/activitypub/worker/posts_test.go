package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestCreatePostStoresLocalNoteAndPublishesConnectorEvent(t *testing.T) {
	local := &actors.Actor{
		ID:       "relay",
		Username: "relay",
		URI:      "https://rosmarinus.example/users/relay",
	}
	noteRepo := &fakeNoteRepo{}
	followRepo := &fakeFollowRepo{}
	remoteHost := "remote.example"
	_, err := followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:          "remote-follower",
		FolloweeID:          local.ID,
		FollowerURI:         "https://remote.example/users/alice",
		FolloweeURI:         local.URI,
		FollowerHost:        &remoteHost,
		FollowerInbox:       "https://remote.example/users/alice/inbox",
		FollowerSharedInbox: "https://remote.example/inbox",
		Status:              follows.StatusAccepted,
	})
	if err != nil {
		t.Fatalf("Upsert follow returned error: %v", err)
	}
	_, err = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID:          "remote-follower-2",
		FolloweeID:          local.ID,
		FollowerURI:         "https://remote.example/users/bob",
		FolloweeURI:         local.URI,
		FollowerHost:        &remoteHost,
		FollowerInbox:       "https://remote.example/users/bob/inbox",
		FollowerSharedInbox: "https://remote.example/inbox",
		Status:              follows.StatusAccepted,
	})
	if err != nil {
		t.Fatalf("Upsert second follow returned error: %v", err)
	}
	q := &fakeQueue{}
	connectorPublisher := &fakeConnectorPublisher{}
	remoteAlice := &actors.Actor{ID: "remote-follower", Host: &remoteHost, URI: "https://remote.example/users/alice"}
	remoteBob := &actors.Actor{ID: "remote-follower-2", Host: &remoteHost, URI: "https://remote.example/users/bob"}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remotes: map[string]*actors.Actor{remoteAlice.URI: remoteAlice, remoteBob.URI: remoteBob}}, noteRepo, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetConnectorPublisher(connectorPublisher)
	post, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID:    "relay",
		NoteID:     "note-1",
		Text:       "hello from Next.js",
		Visibility: string(domainnotes.VisibilityFollowers),
		Hashtags:   []string{"rosmarinus"},
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if post.ActorID != "relay" || post.NoteID == "" || post.URI != "https://rosmarinus.example/notes/"+post.NoteID {
		t.Fatalf("unexpected post payload: %+v", post)
	}
	if _, err := bson.ObjectIDFromHex(post.NoteID); err != nil {
		t.Fatalf("generated Note ID is not ObjectID hex: %q: %v", post.NoteID, err)
	}
	note, err := noteRepo.FindByID(context.Background(), post.NoteID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if note == nil {
		t.Fatalf("note was not stored")
	}
	if note.AuthorID != local.ID || note.AttributedTo != local.URI || note.Text != "hello from Next.js" {
		t.Fatalf("unexpected note: %+v", note)
	}
	if note.Visibility != domainnotes.VisibilityFollowers || len(note.Hashtags) != 1 || note.Hashtags[0] != "rosmarinus" {
		t.Fatalf("unexpected note metadata: %+v", note)
	}
	if connectorPublisher.post == nil {
		t.Fatalf("post.created event was not published")
	}
	if *connectorPublisher.post != post {
		t.Fatalf("published post = %+v", connectorPublisher.post)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d, want 1", len(q.tasks))
	}
	delivery, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	if !ok {
		t.Fatalf("delivery payload type = %T", q.tasks[0].Payload)
	}
	if delivery.ActorID != local.ID || delivery.To != "https://remote.example/inbox" {
		t.Fatalf("unexpected delivery target: %+v", delivery)
	}
	if delivery.Object["type"] != "Create" || delivery.Object["id"] != note.URI+"/activity" || delivery.Object["actor"] != local.URI {
		t.Fatalf("unexpected Create activity: %#v", delivery.Object)
	}
	object, ok := delivery.Object["object"].(map[string]any)
	if !ok || object["type"] != "Note" || object["id"] != note.URI || object["content"] != note.Text {
		t.Fatalf("unexpected Note object: %#v", delivery.Object["object"])
	}
}

func TestCreatePostStoresAndDeliversLocalQuestion(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remoteHost := "remote.example"
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &remoteHost}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: remote.ID, FolloweeID: local.ID, FollowerURI: remote.URI, FolloweeURI: local.URI,
		FollowerHost: &remoteHost, FollowerSharedInbox: "https://remote.example/inbox", Status: follows.StatusAccepted,
	})
	noteRepo := &fakeNoteRepo{}
	pollRepo := &fakePollRepo{}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetPollRepository(pollRepo)
	created, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "local-poll", Text: "choose",
		Poll: &connector.PollCreateCommand{Choices: []string{"cats", "dogs"}},
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	poll := pollRepo.polls[created.NoteID]
	if poll == nil || poll.AuthorHost != nil || len(poll.Choices) != 2 {
		t.Fatalf("unexpected stored poll: %+v", poll)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d", len(q.tasks))
	}
	payload := q.tasks[0].Payload.(queue.DeliverPayload)
	object := payload.Object["object"].(map[string]any)
	if object["type"] != "Question" || object["oneOf"] == nil {
		t.Fatalf("unexpected delivered Question: %#v", object)
	}
}

func TestCreatePostStoresAndDeliversLocalRenote(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay", OwnerAccountID: "account-1"}
	remote := &actors.Actor{
		ID: "remote-author", URI: "https://remote.example/users/alice", Host: &remoteHost,
		Inbox: "https://remote.example/users/alice/inbox",
	}
	target := &domainnotes.Note{
		ID: "remote-note", URI: "https://remote.example/notes/1", AuthorID: remote.ID,
		AttributedTo: remote.URI, Text: "hello", Visibility: domainnotes.VisibilityHome,
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{target.ID: target, target.URI: target}}
	q := &fakeQueue{}
	publisher := &fakeConnectorPublisher{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetConnectorPublisher(publisher)
	created, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "local-renote", RenoteID: target.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	note, err := noteRepo.FindByID(context.Background(), created.NoteID)
	if err != nil || note == nil {
		t.Fatalf("stored renote = %#v, err=%v", note, err)
	}
	if note.RenoteID != target.ID || note.RenoteURI != target.URI || note.Visibility != domainnotes.VisibilityHome {
		t.Fatalf("unexpected stored renote: %+v", note)
	}
	if publisher.post == nil || publisher.post.NoteID != note.ID {
		t.Fatalf("post.created event = %+v", publisher.post)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d, want 1", len(q.tasks))
	}
	payload := q.tasks[0].Payload.(queue.DeliverPayload)
	if payload.To != remote.Inbox || payload.Object["type"] != "Announce" || payload.Object["id"] != note.URI+"/activity" || payload.Object["object"] != target.URI {
		t.Fatalf("unexpected Announce delivery: %#v", payload)
	}
}

func TestCreatePostRejectsInvalidRenoteTargets(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remoteHost := "remote.example"
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/alice", Host: &remoteHost}
	tests := []struct {
		name   string
		target *domainnotes.Note
	}{
		{name: "pure renote", target: &domainnotes.Note{ID: "target", URI: "https://remote.example/activities/renote", AuthorID: remote.ID, AttributedTo: remote.URI, RenoteID: "original", RenoteURI: "https://remote.example/notes/original", Visibility: domainnotes.VisibilityPublic}},
		{name: "followers-only from another actor", target: &domainnotes.Note{ID: "target", URI: "https://remote.example/notes/private", AuthorID: remote.ID, AttributedTo: remote.URI, Text: "private", Visibility: domainnotes.VisibilityFollowers}},
		{name: "specified", target: &domainnotes.Note{ID: "target", URI: "https://remote.example/notes/direct", AuthorID: remote.ID, AttributedTo: remote.URI, Text: "direct", Visibility: domainnotes.VisibilitySpecified}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{tt.target.ID: tt.target, tt.target.URI: tt.target}}
			h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
				&fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
			if _, err := h.CreatePost(context.Background(), connector.PostCreateCommand{ActorID: local.ID, NoteID: "local-renote", RenoteID: tt.target.ID}); err == nil {
				t.Fatal("invalid renote target was accepted")
			}
			if stored, _ := noteRepo.FindByID(context.Background(), "local-renote"); stored != nil {
				t.Fatalf("invalid renote was stored: %+v", stored)
			}
		})
	}
}

func TestCreatePostRejectsBlockedRenoteTarget(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remoteHost := "remote.example"
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/alice", Host: &remoteHost}
	target := &domainnotes.Note{ID: "target", URI: "https://remote.example/notes/target", AuthorID: remote.ID, AttributedTo: remote.URI, Text: "hello", Visibility: domainnotes.VisibilityPublic}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{target.ID: target, target.URI: target}}
	blockRepo := &fakeBlockRepo{}
	_, _ = blockRepo.Upsert(context.Background(), blocks.Block{BlockerID: remote.ID, BlockeeID: local.ID})
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	if _, err := h.CreatePost(context.Background(), connector.PostCreateCommand{ActorID: local.ID, NoteID: "local-renote", RenoteID: target.ID}); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked renote error, got %v", err)
	}
	if stored, _ := noteRepo.FindByID(context.Background(), "local-renote"); stored != nil {
		t.Fatalf("blocked renote was stored: %+v", stored)
	}
}

func TestCreatePostResolvesLocalCustomEmoji(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	noteRepo := &fakeNoteRepo{}
	emojiRepo := &fakeEmojiRepo{emojis: map[string]*emojis.Emoji{
		"local-party": {
			ID: "local-party", Name: "party", URI: "https://rosmarinus.example/emojis/party",
			PublicURL: "https://rosmarinus.example/media/party", MediaType: "image/webp", UpdatedAt: time.Now().UTC(),
		},
	}}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetEmojiRepository(emojiRepo)
	created, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "emoji-note", Text: "hello :party:", EmojiNames: []string{"party"},
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	note, err := noteRepo.FindByID(context.Background(), created.NoteID)
	if err != nil || note == nil || len(note.Emojis) != 1 || note.Emojis[0].Name != "party" {
		t.Fatalf("local emoji was not stored on Note: note=%+v err=%v", note, err)
	}
	rendered := apnotes.Render(note)
	tags, ok := rendered["tag"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("rendered tags = %#v", rendered["tag"])
	}
	tag, _ := tags[0].(map[string]any)
	icon, _ := tag["icon"].(map[string]any)
	if tag["type"] != "Emoji" || tag["name"] != ":party:" || icon["url"] != "https://rosmarinus.example/media/party" {
		t.Fatalf("rendered emoji tag = %#v", tag)
	}
}

func TestVotePollDeliversReplyNoteToRemoteOwner(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "local", URI: "https://local.example/users/bob"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/alice", Host: &remoteHost, Inbox: "https://remote.example/users/alice/inbox"}
	note := &domainnotes.Note{ID: "remote-poll", URI: "https://remote.example/notes/poll", AuthorID: remote.ID, AttributedTo: remote.URI, Visibility: domainnotes.VisibilityPublic}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{note.ID: note, note.URI: note}}
	pollRepo := &fakePollRepo{polls: map[string]*domainpolls.Poll{
		note.ID: {NoteID: note.ID, AuthorID: remote.ID, AuthorHost: &remoteHost, Choices: []string{"cats", "dogs"}, Votes: []int{0, 0}},
	}}
	q := &fakeQueue{}
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetPollRepository(pollRepo)
	result, err := h.VotePoll(context.Background(), connector.PollVoteCommand{ActorID: local.ID, NoteID: note.ID, Choice: 1})
	if err != nil {
		t.Fatalf("VotePoll returned error: %v", err)
	}
	if result.Choice != 1 || result.URI == "" || len(q.tasks) != 1 {
		t.Fatalf("unexpected result=%+v tasks=%+v", result, q.tasks)
	}
	payload := q.tasks[0].Payload.(queue.DeliverPayload)
	object := payload.Object["object"].(map[string]any)
	if payload.To != remote.Inbox || payload.Object["type"] != "Create" || object["name"] != "dogs" || object["inReplyTo"] != note.URI {
		t.Fatalf("unexpected vote delivery: %#v", payload)
	}
}

func TestCreatePostPaginatesFollowerDeliveries(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	followRepo := &fakeFollowRepo{}
	remoteHost := "remote.example"
	remoteActors := make(map[string]*actors.Actor, postDeliveryFollowerLimit*2+5)
	for i := 0; i < postDeliveryFollowerLimit*2+5; i++ {
		id := fmt.Sprintf("follow-%03d", i)
		actorID := "remote-" + id
		actorURI := "https://remote.example/users/" + id
		remoteActors[actorURI] = &actors.Actor{ID: actorID, URI: actorURI, Host: &remoteHost}
		_, err := followRepo.Upsert(context.Background(), follows.Follow{
			ID:                  id,
			FollowerID:          actorID,
			FolloweeID:          local.ID,
			FollowerURI:         actorURI,
			FollowerHost:        &remoteHost,
			FolloweeURI:         local.URI,
			FollowerSharedInbox: "https://remote.example/inbox/" + id,
			Status:              follows.StatusAccepted,
		})
		if err != nil {
			t.Fatalf("Upsert follow %d returned error: %v", i, err)
		}
	}
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remotes: remoteActors}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID,
		NoteID:  "paginated-note",
		Text:    "hello everyone",
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if len(q.tasks) != postDeliveryFollowerLimit*2+5 {
		t.Fatalf("delivery task count = %d, want %d", len(q.tasks), postDeliveryFollowerLimit*2+5)
	}
}

func TestDeletePostSoftDeletesAndDeliversTombstone(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{ID: "remote-follower", URI: "https://remote.example/users/alice", Host: &remoteHost}
	noteRepo := &fakeNoteRepo{}
	note, err := noteRepo.CreateLocalNote(context.Background(), domainnotes.Note{
		ID: "note-to-delete", URI: "https://rosmarinus.example/notes/note-to-delete",
		AuthorID: local.ID, AttributedTo: local.URI, Text: "obsolete", Visibility: domainnotes.VisibilityPublic,
	})
	if err != nil {
		t.Fatalf("CreateLocalNote returned error: %v", err)
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: remote.ID, FolloweeID: local.ID, FollowerURI: remote.URI, FolloweeURI: local.URI,
		FollowerHost: &remoteHost, FollowerSharedInbox: "https://remote.example/inbox", Status: follows.StatusAccepted,
	})
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	cleanupRepo := &fakeAccountCleanupRepo{}
	h.SetAccountCleanupRepository(cleanupRepo)
	deleted, err := h.DeletePost(context.Background(), connector.PostDeleteCommand{ActorID: local.ID, NoteID: note.ID})
	if err != nil {
		t.Fatalf("DeletePost returned error: %v", err)
	}
	if deleted.NoteID != note.ID || deleted.ActorID != local.ID || deleted.URI != note.URI {
		t.Fatalf("unexpected result: %+v", deleted)
	}
	if active, _ := noteRepo.FindByID(context.Background(), note.ID); active != nil {
		t.Fatalf("deleted note remains active: %+v", active)
	}
	if cleanupRepo.noteID != note.ID {
		t.Fatalf("note dependencies were not cleaned: %+v", cleanupRepo)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d", len(q.tasks))
	}
	payload, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	if !ok || payload.To != "https://remote.example/inbox" || payload.Object["type"] != "Delete" {
		t.Fatalf("unexpected delivery: %#v", q.tasks[0])
	}
	if payload.Object["id"] != note.URI+"#delete" {
		t.Fatalf("Delete activity id = %#v", payload.Object["id"])
	}
	tombstone, ok := payload.Object["object"].(map[string]any)
	if !ok || tombstone["type"] != "Tombstone" || tombstone["id"] != note.URI {
		t.Fatalf("unexpected tombstone: %#v", payload.Object["object"])
	}
}

func TestDeletePostDeliversToRemoteReferenceAuthors(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	note := &domainnotes.Note{
		ID: "referenced-note", URI: "https://rosmarinus.example/notes/referenced-note",
		AuthorID: local.ID, AttributedTo: local.URI, Text: "obsolete", Visibility: domainnotes.VisibilityPublic,
	}
	remoteActors := make(map[string]*actors.Actor)
	notesByURI := map[string]*domainnotes.Note{note.URI: note}
	for i, kind := range []string{"reply", "renote", "quote"} {
		host := fmt.Sprintf("remote-%d.example", i)
		uri := "https://" + host + "/users/alice"
		remoteActors[uri] = &actors.Actor{
			ID: "remote-" + kind, URI: uri, Host: &host, Inbox: "https://" + host + "/inbox",
		}
		reference := &domainnotes.Note{
			ID: "remote-" + kind + "-note", URI: "https://" + host + "/notes/1",
			AuthorID: "remote-" + kind, AttributedTo: uri, Visibility: domainnotes.VisibilityPublic,
		}
		switch kind {
		case "reply":
			reference.ReplyID = note.ID
		case "renote":
			reference.RenoteID = note.ID
		case "quote":
			reference.QuoteID = note.ID
		}
		notesByURI[reference.URI] = reference
	}
	noteRepo := &fakeNoteRepo{notes: notesByURI}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remotes: remoteActors}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetAccountCleanupRepository(&fakeAccountCleanupRepo{})

	if _, err := h.DeletePost(context.Background(), connector.PostDeleteCommand{ActorID: local.ID, NoteID: note.ID}); err != nil {
		t.Fatalf("DeletePost returned error: %v", err)
	}
	if len(q.tasks) != 3 {
		t.Fatalf("delivery task count = %d, want 3", len(q.tasks))
	}
	recipients := make(map[string]struct{})
	for _, task := range q.tasks {
		payload, ok := task.Payload.(queue.DeliverPayload)
		if !ok || payload.Object["type"] != "Delete" {
			t.Fatalf("unexpected delivery: %#v", task)
		}
		recipients[payload.To] = struct{}{}
	}
	for i := range 3 {
		inbox := fmt.Sprintf("https://remote-%d.example/inbox", i)
		if _, ok := recipients[inbox]; !ok {
			t.Fatalf("missing concerned recipient %s: %#v", inbox, recipients)
		}
	}
}

func TestActiveReferenceAuthorURIsPaginates(t *testing.T) {
	noteRepo := &fakeNoteRepo{notes: make(map[string]*domainnotes.Note)}
	for i := 0; i < postDeliveryFollowerLimit*2+5; i++ {
		uri := fmt.Sprintf("https://remote.example/users/%03d", i)
		noteRepo.notes[uri] = &domainnotes.Note{ID: fmt.Sprintf("reply-%03d", i), AttributedTo: uri, ReplyID: "target"}
	}
	h := &Handler{notes: noteRepo}
	result, err := h.activeReferenceAuthorURIs(context.Background(), "target")
	if err != nil {
		t.Fatalf("activeReferenceAuthorURIs returned error: %v", err)
	}
	if len(result) != postDeliveryFollowerLimit*2+5 {
		t.Fatalf("reference author count = %d", len(result))
	}
}

func TestDeletePostDeliversUndoAnnounceForLocalRenote(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{ID: "remote-author", URI: "https://remote.example/users/alice", Host: &remoteHost, Inbox: "https://remote.example/users/alice/inbox"}
	target := &domainnotes.Note{ID: "target", URI: "https://remote.example/notes/target", AuthorID: remote.ID, AttributedTo: remote.URI, Text: "hello", Visibility: domainnotes.VisibilityPublic}
	renote := &domainnotes.Note{ID: "renote", URI: "https://rosmarinus.example/notes/renote", AuthorID: local.ID, AttributedTo: local.URI, RenoteID: target.ID, RenoteURI: target.URI, Visibility: domainnotes.VisibilityPublic}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{target.ID: target, target.URI: target, renote.ID: renote, renote.URI: renote}}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	if _, err := h.DeletePost(context.Background(), connector.PostDeleteCommand{ActorID: local.ID, NoteID: renote.ID}); err != nil {
		t.Fatalf("DeletePost returned error: %v", err)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d, want 1", len(q.tasks))
	}
	payload := q.tasks[0].Payload.(queue.DeliverPayload)
	if payload.To != remote.Inbox || payload.Object["type"] != "Undo" || payload.Object["id"] != renote.URI+"/activity/undo" {
		t.Fatalf("unexpected Undo delivery: %#v", payload)
	}
	object, ok := payload.Object["object"].(map[string]any)
	if !ok || object["type"] != "Announce" || object["id"] != renote.URI+"/activity" || object["object"] != target.URI {
		t.Fatalf("unexpected embedded Announce: %#v", payload.Object["object"])
	}
}

func TestCreatePostSkipsBlockedAndSuspendedFollowers(t *testing.T) {
	remoteHost := "remote.example"
	blockedHost := "social.blocked.example"
	suspendedHost := "suspended.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	followRepo := &fakeFollowRepo{}
	for _, follower := range []struct {
		id, inbox string
		host      *string
	}{
		{id: "relationship-blocked", inbox: "https://remote.example/inbox", host: &remoteHost},
		{id: "host-blocked", inbox: "https://social.blocked.example/inbox", host: &blockedHost},
		{id: "suspended", inbox: "https://suspended.example/inbox", host: &suspendedHost},
	} {
		_, _ = followRepo.Upsert(context.Background(), follows.Follow{
			ID: follower.id, FollowerID: follower.id, FolloweeID: local.ID,
			FollowerHost: follower.host, FollowerSharedInbox: follower.inbox, Status: follows.StatusAccepted,
		})
	}
	blockRepo := &fakeBlockRepo{}
	_, _ = blockRepo.Upsert(context.Background(), blocks.Block{BlockerID: "relationship-blocked", BlockeeID: local.ID})
	q := &fakeQueue{}
	activeBlocked := &actors.Actor{ID: "relationship-blocked", URI: "https://remote.example/users/blocked", Host: &remoteHost}
	activeHostBlocked := &actors.Actor{ID: "host-blocked", URI: "https://social.blocked.example/users/alice", Host: &blockedHost}
	suspended := &actors.Actor{ID: "suspended", URI: "https://suspended.example/users/alice", Host: &suspendedHost, IsSuspended: true}
	h := New(config.Config{
		PublicURL: "https://rosmarinus.example", FederationBlockedHosts: []string{"blocked.example"},
	}, nil, &fakeRepo{local: local, remotes: map[string]*actors.Actor{
		activeBlocked.URI: activeBlocked, activeHostBlocked.URI: activeHostBlocked, suspended.URI: suspended,
	}}, &fakeNoteRepo{}, followRepo, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)

	if _, err := h.CreatePost(context.Background(), connector.PostCreateCommand{ActorID: local.ID, NoteID: "blocked-fanout", Text: "hello"}); err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if len(q.tasks) != 0 {
		t.Fatalf("blocked followers received deliveries: %+v", q.tasks)
	}
}

func TestCreateFollowRejectsBlockedRelationship(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local", URI: "https://local.example/users/alice"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &host, Inbox: "https://remote.example/inbox", LastFetchedAt: time.Now()}
	blockRepo := &fakeBlockRepo{}
	_, _ = blockRepo.Upsert(context.Background(), blocks.Block{BlockerID: remote.ID, BlockeeID: local.ID})
	h := New(config.Config{PublicURL: "https://local.example"}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)

	_, err := h.CreateFollow(context.Background(), local.ID, remote.URI)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked follow error, got %v", err)
	}
}

func TestCreatePostDeliversSpecifiedPostToRemoteActorInbox(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{
		ID:          "remote-bob",
		Host:        &remoteHost,
		URI:         "https://remote.example/users/bob",
		Inbox:       "https://remote.example/users/bob/inbox",
		SharedInbox: "https://remote.example/inbox",
	}
	noteRepo := &fakeNoteRepo{}
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	created, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID:    local.ID,
		NoteID:     "specified-note",
		Text:       "hello Bob",
		Visibility: string(domainnotes.VisibilitySpecified),
		MentionURIs: []string{
			" " + remote.URI + " ",
			remote.URI,
		},
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d, want 1", len(q.tasks))
	}
	delivery, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	if !ok {
		t.Fatalf("delivery payload type = %T", q.tasks[0].Payload)
	}
	if delivery.To != remote.Inbox {
		t.Fatalf("delivery target = %q, want personal inbox %q", delivery.To, remote.Inbox)
	}
	object, ok := delivery.Object["object"].(map[string]any)
	if !ok {
		t.Fatalf("object = %#v", delivery.Object["object"])
	}
	to, ok := object["to"].([]string)
	if !ok || len(to) != 1 || to[0] != remote.URI {
		t.Fatalf("object audience = %#v", object["to"])
	}
	note, err := noteRepo.FindByID(context.Background(), created.NoteID)
	if err != nil || note == nil {
		t.Fatalf("stored note = %#v, err=%v", note, err)
	}
	if len(note.MentionURIs) != 1 || note.MentionURIs[0] != remote.URI {
		t.Fatalf("stored mention URIs = %#v", note.MentionURIs)
	}
}

func TestCreatePostDeliversPublicMentionToNonFollower(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{
		ID: "remote-bob", Host: &remoteHost, URI: "https://remote.example/users/bob",
		Inbox: "https://remote.example/users/bob/inbox",
	}
	q := &fakeQueue{}
	noteRepo := &fakeNoteRepo{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "public-mention", Text: "hello Bob", MentionURIs: []string{remote.URI},
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d", len(q.tasks))
	}
	payload, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	if !ok || payload.To != remote.Inbox || payload.Object["type"] != "Create" {
		t.Fatalf("unexpected public mention delivery: %#v", q.tasks[0])
	}
}

func TestCreatePostResolvesAndDeliversReplyAndQuoteTargets(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	replyHost := "reply.example"
	quoteHost := "quote.example"
	replyAuthor := &actors.Actor{
		ID: "reply-author", Host: &replyHost, URI: "https://reply.example/users/alice", Inbox: "https://reply.example/inbox",
	}
	quoteAuthor := &actors.Actor{
		ID: "quote-author", Host: &quoteHost, URI: "https://quote.example/users/bob", Inbox: "https://quote.example/inbox",
	}
	replyTarget := &domainnotes.Note{
		ID: "reply-target", URI: "https://reply.example/notes/1", AuthorID: replyAuthor.ID,
		AttributedTo: replyAuthor.URI, Text: "reply target", Visibility: domainnotes.VisibilityPublic,
	}
	quoteTarget := &domainnotes.Note{
		ID: "quote-target", URI: "https://quote.example/notes/1", AuthorID: quoteAuthor.ID,
		AttributedTo: quoteAuthor.URI, Text: "quote target", Visibility: domainnotes.VisibilityHome,
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		replyTarget.URI: replyTarget,
		quoteTarget.URI: quoteTarget,
	}}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remotes: map[string]*actors.Actor{replyAuthor.URI: replyAuthor, quoteAuthor.URI: quoteAuthor}},
		noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)

	created, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "reply-and-quote", Text: "both",
		InReplyToURI: " " + replyTarget.URI + " ", QuoteURI: quoteTarget.URI,
		Visibility: string(domainnotes.VisibilityPublic),
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	stored, err := noteRepo.FindByID(context.Background(), created.NoteID)
	if err != nil || stored == nil {
		t.Fatalf("stored note = %#v, err=%v", stored, err)
	}
	if stored.ReplyID != replyTarget.ID || stored.InReplyToURI != replyTarget.URI || stored.QuoteID != quoteTarget.ID || stored.QuoteURI != quoteTarget.URI {
		t.Fatalf("stored references = %#v", stored)
	}
	if stored.Visibility != domainnotes.VisibilityHome {
		t.Fatalf("visibility = %q, want home", stored.Visibility)
	}
	if len(q.tasks) != 2 {
		t.Fatalf("delivery task count = %d, want 2", len(q.tasks))
	}
	recipients := map[string]struct{}{}
	for _, task := range q.tasks {
		payload, ok := task.Payload.(queue.DeliverPayload)
		if !ok || payload.Object["type"] != "Create" {
			t.Fatalf("unexpected delivery: %#v", task)
		}
		recipients[payload.To] = struct{}{}
	}
	for _, inbox := range []string{replyAuthor.Inbox, quoteAuthor.Inbox} {
		if _, ok := recipients[inbox]; !ok {
			t.Fatalf("missing direct recipient %s: %#v", inbox, recipients)
		}
	}
}

func TestCreateSpecifiedReplyUsesTargetAsVisibleRecipient(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remoteHost := "remote.example"
	remote := &actors.Actor{
		ID: "remote-alice", Host: &remoteHost, URI: "https://remote.example/users/alice", Inbox: "https://remote.example/inbox",
	}
	target := &domainnotes.Note{
		ID: "target", URI: "https://remote.example/notes/target", AuthorID: remote.ID,
		AttributedTo: remote.URI, Visibility: domainnotes.VisibilityPublic,
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{target.URI: target}}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)

	created, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "specified-reply", Text: "private reply",
		InReplyToURI: target.URI, Visibility: string(domainnotes.VisibilitySpecified),
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	stored, _ := noteRepo.FindByID(context.Background(), created.NoteID)
	if stored == nil || len(stored.MentionURIs) != 0 || len(stored.VisibleUserURIs) != 1 || stored.VisibleUserURIs[0] != remote.URI {
		t.Fatalf("stored specified reply = %#v", stored)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d", len(q.tasks))
	}
	payload, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	object, objectOK := payload.Object["object"].(map[string]any)
	to, toOK := object["to"].([]string)
	if !ok || !objectOK || !toOK || len(to) != 1 || to[0] != remote.URI || payload.To != remote.Inbox {
		t.Fatalf("unexpected specified reply delivery: %#v", q.tasks[0])
	}
}

func TestCreatePostRejectsUnshareableQuote(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remoteHost := "remote.example"
	remote := &actors.Actor{ID: "remote-alice", Host: &remoteHost, URI: "https://remote.example/users/alice"}
	target := &domainnotes.Note{
		ID: "followers-target", URI: "https://remote.example/notes/followers", AuthorID: remote.ID,
		AttributedTo: remote.URI, Visibility: domainnotes.VisibilityFollowers,
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{target.URI: target}}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		FollowerID: local.ID, FolloweeID: remote.ID, FollowerURI: local.URI, FolloweeURI: remote.URI, Status: follows.StatusAccepted,
	})
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil,
		&fakeRepo{local: local, remote: remote}, noteRepo, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)

	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "invalid-quote", Text: "leak", QuoteURI: target.URI,
	})
	if err == nil || !strings.Contains(err.Error(), "not shareable") {
		t.Fatalf("expected unshareable quote error, got %v", err)
	}
	if stored, _ := noteRepo.FindByID(context.Background(), "invalid-quote"); stored != nil {
		t.Fatalf("invalid quote was stored: %#v", stored)
	}
}

func TestCreatePostRejectsSpecifiedPostWithoutRecipients(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	noteRepo := &fakeNoteRepo{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{local: local}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID:    local.ID,
		NoteID:     "invalid-specified-note",
		Text:       "nobody",
		Visibility: string(domainnotes.VisibilitySpecified),
	})
	if err == nil {
		t.Fatal("expected specified post without recipients to fail")
	}
	note, findErr := noteRepo.FindByID(context.Background(), "invalid-specified-note")
	if findErr != nil {
		t.Fatalf("FindByID returned error: %v", findErr)
	}
	if note != nil {
		t.Fatalf("invalid note was stored: %#v", note)
	}
}

func TestCreateReactionStoresAndDeliversLike(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{
		ID:  "relay",
		URI: "https://rosmarinus.example/users/relay",
	}
	remote := &actors.Actor{
		ID:    "remote-alice",
		Host:  &remoteHost,
		URI:   "https://remote.example/users/alice",
		Inbox: "https://remote.example/users/alice/inbox",
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"remote-note": {
			ID:           "remote-note",
			URI:          "https://remote.example/notes/1",
			AttributedTo: remote.URI,
			AuthorID:     remote.ID,
			Visibility:   domainnotes.VisibilityPublic,
		},
	}}
	reactionRepo := &fakeReactionRepo{}
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, reactionRepo, &fakeReportRepo{}, q, &fakeClient{}, local)
	created, err := h.CreateReaction(context.Background(), connector.ReactionCreateCommand{
		ActorID:  local.ID,
		NoteID:   "remote-note",
		Reaction: " 👍 ",
	})
	if err != nil {
		t.Fatalf("CreateReaction returned error: %v", err)
	}
	if created.ReactionID == "" || created.NoteID != "remote-note" || created.Reaction != "👍" || created.URI != "https://rosmarinus.example/likes/"+created.ReactionID {
		t.Fatalf("created reaction = %+v", created)
	}
	stored, err := reactionRepo.Find(context.Background(), "remote-note", local.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored reaction = %+v, err=%v", stored, err)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("delivery task count = %d, want 1", len(q.tasks))
	}
	delivery, ok := q.tasks[0].Payload.(queue.DeliverPayload)
	if !ok {
		t.Fatalf("delivery payload type = %T", q.tasks[0].Payload)
	}
	if delivery.ActorID != local.ID || delivery.To != remote.Inbox || delivery.Object["type"] != "Like" || delivery.Object["object"] != stored.NoteURI || delivery.Object["_misskey_reaction"] != "👍" {
		t.Fatalf("delivery = %+v", delivery)
	}
	deleted, err := h.DeleteReaction(context.Background(), connector.ReactionDeleteCommand{
		ActorID: local.ID,
		NoteID:  "remote-note",
	})
	if err != nil {
		t.Fatalf("DeleteReaction returned error: %v", err)
	}
	if deleted.ReactionID != created.ReactionID || deleted.NoteID != created.NoteID || deleted.URI != created.URI+"/undo" {
		t.Fatalf("deleted reaction = %+v", deleted)
	}
	stored, err = reactionRepo.Find(context.Background(), "remote-note", local.ID)
	if err != nil || stored != nil {
		t.Fatalf("reaction after delete = %+v, err=%v", stored, err)
	}
	if len(q.tasks) != 2 {
		t.Fatalf("delivery task count after delete = %d, want 2", len(q.tasks))
	}
	undoDelivery, ok := q.tasks[1].Payload.(queue.DeliverPayload)
	if !ok {
		t.Fatalf("Undo delivery payload type = %T", q.tasks[1].Payload)
	}
	like, ok := undoDelivery.Object["object"].(map[string]any)
	if undoDelivery.ActorID != local.ID || undoDelivery.To != remote.Inbox || undoDelivery.Object["type"] != "Undo" || !ok || like["type"] != "Like" || like["id"] != created.URI {
		t.Fatalf("Undo delivery = %+v", undoDelivery)
	}
}

func TestCreateReactionRejectsNoteInvisibleToActor(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{
		ID:    "remote-alice",
		Host:  &remoteHost,
		URI:   "https://remote.example/users/alice",
		Inbox: "https://remote.example/users/alice/inbox",
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"private-note": {
			ID:           "private-note",
			URI:          "https://remote.example/notes/private",
			AttributedTo: remote.URI,
			AuthorID:     remote.ID,
			Visibility:   domainnotes.VisibilitySpecified,
			MentionURIs:  []string{"https://elsewhere.example/users/bob"},
		},
	}}
	reactionRepo := &fakeReactionRepo{}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, reactionRepo, &fakeReportRepo{}, q, &fakeClient{}, local)
	_, err := h.CreateReaction(context.Background(), connector.ReactionCreateCommand{
		ActorID:  local.ID,
		NoteID:   "private-note",
		Reaction: "👍",
	})
	if err == nil {
		t.Fatal("expected invisible Note reaction to fail")
	}
	if len(reactionRepo.reactions) != 0 || len(q.tasks) != 0 {
		t.Fatalf("invisible reaction mutated state: reactions=%+v tasks=%+v", reactionRepo.reactions, q.tasks)
	}
}

func TestCreateReactionDeliversLocalCustomEmojiTag(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{ID: "remote-alice", Host: &remoteHost, URI: "https://remote.example/users/alice", Inbox: "https://remote.example/users/alice/inbox"}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"remote-note": {ID: "remote-note", URI: "https://remote.example/notes/1", AttributedTo: remote.URI, AuthorID: remote.ID, Visibility: domainnotes.VisibilityPublic},
	}}
	emojiRepo := &fakeEmojiRepo{emojis: map[string]*emojis.Emoji{
		"local-party": {Name: "party", PublicURL: "https://cdn.rosmarinus.example/party.webp", MediaType: "image/webp"},
	}}
	q := &fakeQueue{}
	reactionRepo := &fakeReactionRepo{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute}}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, reactionRepo, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetEmojiRepository(emojiRepo)

	created, err := h.CreateReaction(context.Background(), connector.ReactionCreateCommand{ActorID: local.ID, NoteID: "remote-note", Reaction: ":party:"})
	if err != nil {
		t.Fatalf("CreateReaction returned error: %v", err)
	}
	if created.Reaction != ":party@.:" {
		t.Fatalf("normalized reaction = %q", created.Reaction)
	}
	storedReaction, err := reactionRepo.Find(context.Background(), "remote-note", local.ID)
	if err != nil || storedReaction == nil || storedReaction.EmojiName != "party" || storedReaction.EmojiURL != "https://cdn.rosmarinus.example/party.webp" {
		t.Fatalf("stored local reaction emoji = %+v, err=%v", storedReaction, err)
	}
	delivery := q.tasks[0].Payload.(queue.DeliverPayload)
	tags, ok := delivery.Object["tag"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("delivery emoji tags = %#v", delivery.Object["tag"])
	}
	tag := tags[0].(map[string]any)
	icon := tag["icon"].(map[string]any)
	if tag["name"] != ":party:" || icon["url"] != "https://cdn.rosmarinus.example/party.webp" || delivery.Object["_misskey_reaction"] != ":party@.:" {
		t.Fatalf("delivery = %#v", delivery.Object)
	}

	if _, err := h.DeleteReaction(context.Background(), connector.ReactionDeleteCommand{ActorID: local.ID, NoteID: "remote-note"}); err != nil {
		t.Fatalf("DeleteReaction returned error: %v", err)
	}
	undo := q.tasks[1].Payload.(queue.DeliverPayload).Object
	like := undo["object"].(map[string]any)
	if _, ok := like["tag"]; !ok {
		t.Fatalf("Undo embedded Like omitted emoji tag: %#v", like)
	}
}

func TestCreateReactionRejectsUnknownLocalCustomEmoji(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	remote := &actors.Actor{ID: "remote-alice", Host: &remoteHost, URI: "https://remote.example/users/alice", Inbox: "https://remote.example/users/alice/inbox"}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"remote-note": {ID: "remote-note", URI: "https://remote.example/notes/1", AttributedTo: remote.URI, AuthorID: remote.ID, Visibility: domainnotes.VisibilityPublic},
	}}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetEmojiRepository(&fakeEmojiRepo{})

	if _, err := h.CreateReaction(context.Background(), connector.ReactionCreateCommand{ActorID: local.ID, NoteID: "remote-note", Reaction: ":missing:"}); err == nil {
		t.Fatal("expected unknown local custom emoji to fail")
	}
}

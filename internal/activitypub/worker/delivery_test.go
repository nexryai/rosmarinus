package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/instances"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestHandleDeliverTaskTracksInstanceAndSkipsSuspendedHost(t *testing.T) {
	local := &actors.Actor{ID: "local-id", URI: "https://local.example/users/alice"}
	newTask := func(t *testing.T) *asynq.Task {
		t.Helper()
		payload, err := json.Marshal(queue.DeliverPayload{
			Version: 1, ActorID: local.ID, To: "https://remote.example/inbox",
			Object: map[string]any{"type": "Create"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return asynq.NewTask(queue.TaskDeliver, payload)
	}

	t.Run("success", func(t *testing.T) {
		client := &fakeClient{}
		instanceRepo := &fakeInstanceRepo{}
		queued := &fakeQueue{}
		h := &Handler{repo: &fakeRepo{local: local}, client: client, instances: instanceRepo, queue: queued}
		if err := h.HandleDeliverTask(context.Background(), newTask(t)); err != nil {
			t.Fatalf("HandleDeliverTask returned error: %v", err)
		}
		if client.deliveries != 1 || instanceRepo.success != 1 || instanceRepo.instance.LatestStatus != http.StatusAccepted {
			t.Fatalf("delivery state was not recorded: client=%+v instance=%+v", client, instanceRepo.instance)
		}
		if queued.task.Type != queue.TaskMetadata {
			t.Fatalf("metadata refresh was not scheduled: %+v", queued.task)
		}
	})

	t.Run("failure", func(t *testing.T) {
		client := &fakeClient{deliverErr: deliveryStatusError{status: http.StatusServiceUnavailable}}
		instanceRepo := &fakeInstanceRepo{}
		h := &Handler{repo: &fakeRepo{local: local}, client: client, instances: instanceRepo}
		if err := h.HandleDeliverTask(context.Background(), newTask(t)); err == nil {
			t.Fatal("failed delivery returned nil")
		}
		if instanceRepo.failure != 1 || instanceRepo.instance.LatestStatus != http.StatusServiceUnavailable || !instanceRepo.instance.IsNotResponding {
			t.Fatalf("failure state was not recorded: %+v", instanceRepo.instance)
		}
	})

	t.Run("suspended", func(t *testing.T) {
		client := &fakeClient{}
		instanceRepo := &fakeInstanceRepo{instance: &instances.Instance{Host: "remote.example", SuspensionState: instances.SuspensionManual}}
		h := &Handler{repo: &fakeRepo{local: local}, client: client, instances: instanceRepo}
		if err := h.HandleDeliverTask(context.Background(), newTask(t)); err != nil {
			t.Fatalf("HandleDeliverTask returned error: %v", err)
		}
		if client.deliveries != 0 {
			t.Fatalf("delivery to suspended host was attempted: %+v", client)
		}
	})

	t.Run("gone shared inbox", func(t *testing.T) {
		client := &fakeClient{deliverErr: deliveryStatusError{status: http.StatusGone}}
		instanceRepo := &fakeInstanceRepo{}
		h := &Handler{repo: &fakeRepo{local: local}, client: client, instances: instanceRepo}
		payload, err := json.Marshal(queue.DeliverPayload{
			Version: 1, ActorID: local.ID, To: "https://remote.example/inbox",
			Object: map[string]any{"type": "Create"}, IsSharedInbox: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		err = h.HandleDeliverTask(context.Background(), asynq.NewTask(queue.TaskDeliver, payload))
		if !errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("gone shared inbox error = %v, want SkipRetry", err)
		}
		if instanceRepo.instance.SuspensionState != instances.SuspensionGone {
			t.Fatalf("instance state = %q", instanceRepo.instance.SuspensionState)
		}
	})
}

func TestHandleMetadataTaskUsesDailyFreshnessAndHostLock(t *testing.T) {
	metadata := instances.Metadata{NodeInfoFetched: true, SoftwareName: "Misskey", SoftwareVersion: "2026.8.0", Name: "Remote"}
	fetcher := &fakeInstanceMetadataFetcher{metadata: metadata}
	instanceRepo := &fakeInstanceRepo{}
	locker := &fakeActivityLocker{acquired: true}
	h := &Handler{instances: instanceRepo, metadataFetcher: fetcher, locker: locker}
	payload, err := json.Marshal(queue.MetadataPayload{Version: 1, Host: "remote.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.HandleMetadataTask(context.Background(), asynq.NewTask(queue.TaskMetadata, payload)); err != nil {
		t.Fatalf("HandleMetadataTask returned error: %v", err)
	}
	if fetcher.calls != 1 || instanceRepo.metadata.SoftwareName != "Misskey" || locker.name != "metadata:remote.example" || !locker.unlocked {
		t.Fatalf("metadata update did not use the host lock: fetches=%d repo=%+v lock=%+v", fetcher.calls, instanceRepo.metadata, locker)
	}

	fresh := time.Now().UTC()
	instanceRepo.instance.InfoUpdatedAt = &fresh
	locker.name = ""
	if err := h.HandleMetadataTask(context.Background(), asynq.NewTask(queue.TaskMetadata, payload)); err != nil {
		t.Fatalf("fresh HandleMetadataTask returned error: %v", err)
	}
	if fetcher.calls != 1 || locker.name != "" {
		t.Fatalf("fresh metadata was fetched or locked: calls=%d lock=%q", fetcher.calls, locker.name)
	}
}

func TestPerformCollectionProcessesBoundedSignerHostedActivities(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote-alice", URI: "https://remote.example/users/alice", Host: &host}
	noteRepo := &fakeNoteRepo{}
	client := &fakeClient{objects: map[string]map[string]any{
		"https://remote.example/activities/2": {
			"id":    "https://remote.example/activities/2",
			"type":  "Create",
			"actor": remote.URI,
			"object": map[string]any{
				"id":           "https://remote.example/notes/2",
				"type":         "Note",
				"attributedTo": remote.URI,
				"to":           apnotes.PublicAudience,
				"content":      "second",
			},
		},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id":   "https://remote.example/collections/inbox",
		"type": "OrderedCollection",
		"orderedItems": []any{
			map[string]any{
				"id":    "https://remote.example/activities/1",
				"type":  "Create",
				"actor": remote.URI,
				"object": map[string]any{
					"id":           "https://remote.example/notes/1",
					"type":         "Note",
					"attributedTo": remote.URI,
					"to":           apnotes.PublicAudience,
					"content":      "first",
				},
			},
			"https://remote.example/activities/2",
		},
	})
	if err != nil || result != "ok: collection processed" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(noteRepo.notes) != 2 {
		t.Fatalf("stored notes = %#v", noteRepo.notes)
	}
}

func TestPerformCollectionRejectsForeignActivityAndLargeCollection(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote-alice", URI: "https://remote.example/users/alice", Host: &host}
	noteRepo := &fakeNoteRepo{}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id":   "https://remote.example/collections/inbox",
		"type": "Collection",
		"items": []any{map[string]any{
			"id":    "https://evil.example/activities/1",
			"type":  "Create",
			"actor": remote.URI,
		}},
	})
	if err != nil || !strings.Contains(result, "activity id host mismatches signer") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(noteRepo.notes) != 0 {
		t.Fatalf("foreign activity stored notes: %#v", noteRepo.notes)
	}
	result, err = h.performActivity(context.Background(), remote, map[string]any{
		"id":   "https://remote.example/collections/inbox-port",
		"type": "Collection",
		"items": []any{map[string]any{
			"id":    "https://remote.example:8443/activities/1",
			"type":  "Create",
			"actor": remote.URI,
		}},
	})
	if err != nil || !strings.Contains(result, "activity id host mismatches signer") {
		t.Fatalf("different-port result=%q err=%v", result, err)
	}

	items := make([]any, collectionActivityLimit)
	for i := range items {
		items[i] = map[string]any{"id": fmt.Sprintf("https://remote.example/activities/%d", i), "type": "Like"}
	}
	result, err = h.performActivity(context.Background(), remote, map[string]any{
		"id":    "https://remote.example/collections/large",
		"type":  "Collection",
		"items": items,
	})
	if err != nil || result != "skip: collection would surpass recursion limit" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestPerformCreateRejectsNoteOnDifferentActorPort(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote-alice", URI: "https://remote.example:8443/users/alice", Host: &host}
	noteRepo := &fakeNoteRepo{}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id": "https://remote.example:8443/activities/create", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example:9443/notes/1", "type": "Note",
			"attributedTo": remote.URI, "to": apnotes.PublicAudience, "content": "wrong port",
		},
	})
	if err != nil || result != "skip: host in actor.uri !== note.id" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(noteRepo.notes) != 0 {
		t.Fatalf("different-port note was stored: %#v", noteRepo.notes)
	}
}

func TestPerformCollectionTracksNestedAndDuplicateResolution(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote-alice", URI: "https://remote.example/users/alice", Host: &host}
	noteRepo := &fakeNoteRepo{}
	activityURI := "https://remote.example/activities/fetched"
	client := &fakeClient{objects: map[string]map[string]any{
		activityURI: {
			"id": activityURI, "type": "Create", "actor": remote.URI,
			"object": map[string]any{
				"id": "https://remote.example/notes/fetched", "type": "Note",
				"attributedTo": remote.URI, "to": apnotes.PublicAudience, "content": "fetched",
			},
		},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id": "https://remote.example/collections/outer", "type": "Collection",
		"items": []any{
			map[string]any{
				"id": "https://remote.example/collections/nested", "type": "OrderedCollection",
				"orderedItems": []any{map[string]any{
					"id": "https://remote.example/activities/embedded", "type": "Create", "actor": remote.URI,
					"object": map[string]any{
						"id": "https://remote.example/notes/embedded", "type": "Note",
						"attributedTo": remote.URI, "to": apnotes.PublicAudience, "content": "embedded",
					},
				}},
			},
			activityURI,
			activityURI,
		},
	})
	if err != nil || !strings.Contains(result, "cannot resolve already resolved activity") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(noteRepo.notes) != 2 {
		t.Fatalf("stored notes = %#v", noteRepo.notes)
	}
}

func TestPerformCollectionRejectsExcessiveNesting(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote-alice", URI: "https://remote.example/users/alice", Host: &host}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	activity := map[string]any{"id": "https://remote.example/activities/leaf", "type": "Unknown"}
	for i := 0; i <= collectionActivityLimit; i++ {
		activity = map[string]any{
			"id":    fmt.Sprintf("https://remote.example/collections/%d", i),
			"type":  "Collection",
			"items": []any{activity},
		}
	}
	result, err := h.performActivity(context.Background(), remote, activity)
	if err != nil || !strings.Contains(result, "collection would surpass recursion limit") {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestPerformCreatePersistsReplyNotificationWithoutDuplicateMention(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", URI: "https://local.example/users/alice"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &host, LastFetchedAt: time.Now()}
	replyURI := "https://local.example/notes/root"
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		replyURI: {ID: "local-root", URI: replyURI, AuthorID: local.ID, AttributedTo: local.URI},
	}}
	notificationRepo := &fakeNotificationRepo{}
	connectorPublisher := &fakeConnectorPublisher{}
	h := New(config.Config{PublicURL: "https://local.example"}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetNotificationRepository(notificationRepo)
	h.SetConnectorPublisher(connectorPublisher)

	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id": "https://remote.example/activities/create", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example/notes/reply", "type": "Note", "attributedTo": remote.URI,
			"inReplyTo": replyURI, "content": "reply", "to": apnotes.PublicAudience,
			"tag": []any{map[string]any{"type": "Mention", "href": local.URI, "name": "@alice@local.example"}},
		},
	})
	if err != nil || result != "ok: note created" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(notificationRepo.notifications) != 1 || connectorPublisher.notification == nil || connectorPublisher.notification.Kind != notifications.KindReply {
		t.Fatalf("reply notification was not deduplicated: stored=%+v event=%+v", notificationRepo.notifications, connectorPublisher.notification)
	}
}

func TestPerformCreatePublishesRemotePublicNoteToOwnedAccounts(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", URI: "https://local.example/users/alice"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &host, LastFetchedAt: time.Now()}
	publisher := &fakeConnectorPublisher{}
	h := New(config.Config{PublicURL: "https://local.example"}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetConnectorPublisher(publisher)

	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id": "https://remote.example/activities/create-public", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example/notes/public", "type": "Note", "attributedTo": remote.URI,
			"content": "public note", "to": apnotes.PublicAudience,
		},
	})
	if err != nil || result != "ok: note created" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("note.created events = %+v, want one", publisher.posts)
	}
	event := publisher.posts[0]
	if event.AccountID != local.OwnerAccountID || event.ActorID != "" || event.NoteID != "note-id" || event.URI != "https://remote.example/notes/public" {
		t.Fatalf("unexpected public note.created event: %+v", event)
	}
}

func TestPerformCreatePublishesRemoteHomeNoteToLocalFollowers(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", URI: "https://local.example/users/alice"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &host, LastFetchedAt: time.Now()}
	followRepo := &fakeFollowRepo{follows: map[string]*follows.Follow{
		local.ID + "\x00" + remote.ID: {
			ID: "follow-1", FollowerID: local.ID, FolloweeID: remote.ID,
			FollowerURI: local.URI, FolloweeURI: remote.URI, FolloweeHost: &host, Status: follows.StatusAccepted,
		},
	}}
	publisher := &fakeConnectorPublisher{}
	h := New(config.Config{PublicURL: "https://local.example"}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetConnectorPublisher(publisher)

	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id": "https://remote.example/activities/create-home", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example/notes/home", "type": "Note", "attributedTo": remote.URI,
			"content": "home note", "to": remote.URI + "/followers", "cc": apnotes.PublicAudience,
		},
	})
	if err != nil || result != "ok: note created" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(publisher.posts) != 1 {
		t.Fatalf("note.created events = %+v, want one", publisher.posts)
	}
	event := publisher.posts[0]
	if event.AccountID != local.OwnerAccountID || event.ActorID != local.ID || event.NoteID != "note-id" || event.URI != "https://remote.example/notes/home" {
		t.Fatalf("unexpected home note.created event: %+v", event)
	}
}

func TestPerformCreateDoesNotPublishRemoteHomeNoteToNonFollowers(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", URI: "https://local.example/users/alice"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &host, LastFetchedAt: time.Now()}
	publisher := &fakeConnectorPublisher{}
	h := New(config.Config{PublicURL: "https://local.example"}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetConnectorPublisher(publisher)

	result, err := h.performActivity(context.Background(), remote, map[string]any{
		"id": "https://remote.example/activities/create-home", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example/notes/home", "type": "Note", "attributedTo": remote.URI,
			"content": "home note", "to": remote.URI + "/followers", "cc": apnotes.PublicAudience,
		},
	})
	if err != nil || result != "ok: note created" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if len(publisher.posts) != 0 {
		t.Fatalf("home note event leaked to a non-follower: %+v", publisher.posts)
	}
}

func TestPerformCreateStoresRemoteQuestionPoll(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/alice", Host: &host, LastFetchedAt: time.Now()}
	noteRepo := &fakeNoteRepo{}
	pollRepo := &fakePollRepo{}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	h.SetPollRepository(pollRepo)
	result, err := h.performCreate(context.Background(), remote, map[string]any{
		"id": "https://remote.example/activities/create-poll", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example/notes/poll", "type": "Question", "attributedTo": remote.URI,
			"to": apnotes.PublicAudience, "content": "choose", "endTime": "2026-08-25T00:00:00Z",
			"oneOf": []any{
				map[string]any{"name": "cats", "replies": map[string]any{"totalItems": 2}},
				map[string]any{"name": "dogs", "_misskey_votes": 3},
			},
		},
	})
	if err != nil || result != "ok: note created" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	poll := pollRepo.polls["note-id"]
	if poll == nil || poll.Multiple || len(poll.Choices) != 2 || poll.Votes[0] != 2 || poll.Votes[1] != 3 || poll.ExpiresAt == nil {
		t.Fatalf("unexpected poll: %+v", poll)
	}
}

func TestPerformUpdateQuestionChangesOnlyExistingChoiceVotes(t *testing.T) {
	host := "remote.example"
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/alice", Host: &host}
	noteURI := "https://remote.example/notes/poll"
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		noteURI: {ID: "poll-note", URI: noteURI, AuthorID: remote.ID, AttributedTo: remote.URI},
	}}
	pollRepo := &fakePollRepo{polls: map[string]*domainpolls.Poll{
		"poll-note": {NoteID: "poll-note", AuthorID: remote.ID, Choices: []string{"cats", "dogs"}, Votes: []int{1, 1}},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	h.SetPollRepository(pollRepo)
	result, err := h.performUpdate(context.Background(), remote, map[string]any{
		"type": "Update", "actor": remote.URI,
		"object": map[string]any{
			"id": noteURI, "type": "Question", "attributedTo": remote.URI,
			"oneOf": []any{
				map[string]any{"name": "dogs", "replies": map[string]any{"totalItems": 4}},
				map[string]any{"name": "cats", "replies": map[string]any{"totalItems": 3}},
			},
		},
	})
	if err != nil || result != "ok: Question updated" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if got := pollRepo.polls["poll-note"].Votes; len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("votes = %#v", got)
	}
}

func TestPerformAddAndRemoveFeaturedNote(t *testing.T) {
	host := "remote.example"
	actorURI := "https://remote.example/users/alice"
	featuredURI := actorURI + "/collections/featured"
	noteURI := "https://remote.example/notes/pinned"
	remote := &actors.Actor{
		ID: "remote", URI: actorURI, Host: &host, FeaturedURI: featuredURI,
	}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		noteURI: {ID: "pinned-note", URI: noteURI, AuthorID: remote.ID, AttributedTo: remote.URI},
	}}
	actorRepo := &fakeRepo{remote: remote}
	h := New(config.Config{}, nil, actorRepo, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)

	result, err := h.performOneActivity(context.Background(), remote, map[string]any{
		"type": "Add", "actor": actorURI, "target": featuredURI, "object": noteURI,
	})
	if err != nil || result != "ok: featured note added" {
		t.Fatalf("Add result=%q err=%v", result, err)
	}
	if len(remote.FeaturedNoteIDs) != 1 || remote.FeaturedNoteIDs[0] != "pinned-note" {
		t.Fatalf("featured notes after Add = %#v", remote.FeaturedNoteIDs)
	}

	result, err = h.performOneActivity(context.Background(), remote, map[string]any{
		"type": "Remove", "actor": actorURI, "target": featuredURI, "object": noteURI,
	})
	if err != nil || result != "ok: featured note removed" {
		t.Fatalf("Remove result=%q err=%v", result, err)
	}
	if len(remote.FeaturedNoteIDs) != 0 {
		t.Fatalf("featured notes after Remove = %#v", remote.FeaturedNoteIDs)
	}
}

func TestPerformFeaturedChangeRejectsInvalidTargetAndAttribution(t *testing.T) {
	host := "remote.example"
	actorURI := "https://remote.example/users/alice"
	featuredURI := actorURI + "/collections/featured"
	remote := &actors.Actor{ID: "remote", URI: actorURI, Host: &host, FeaturedURI: featuredURI}
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		"https://remote.example/notes/other": {
			ID: "other-note", URI: "https://remote.example/notes/other",
			AuthorID: "other-actor", AttributedTo: "https://remote.example/users/bob",
		},
	}}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)

	result, err := h.performOneActivity(context.Background(), remote, map[string]any{
		"type": "Add", "actor": actorURI,
		"target": actorURI + "/collections/not-featured",
		"object": "https://remote.example/notes/other",
	})
	if err != nil || result != "skip: featured change target is not actor featured collection" {
		t.Fatalf("invalid target result=%q err=%v", result, err)
	}

	result, err = h.performOneActivity(context.Background(), remote, map[string]any{
		"type": "Add", "actor": actorURI, "target": featuredURI,
		"object": "https://remote.example/notes/other",
	})
	if err != nil || result != "skip: featured note attribution mismatch" {
		t.Fatalf("invalid attribution result=%q err=%v", result, err)
	}
	if len(remote.FeaturedNoteIDs) != 0 {
		t.Fatalf("invalid featured change mutated actor: %#v", remote.FeaturedNoteIDs)
	}
}

func TestPerformCreateConsumesRemoteVoteForLocalPoll(t *testing.T) {
	remoteHost := "remote.example"
	local := &actors.Actor{ID: "local", URI: "https://local.example/users/alice"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/bob", Host: &remoteHost}
	pollURI := "https://local.example/notes/poll"
	noteRepo := &fakeNoteRepo{notes: map[string]*domainnotes.Note{
		pollURI: {ID: "poll-note", URI: pollURI, AuthorID: local.ID, AttributedTo: local.URI, Visibility: domainnotes.VisibilityPublic},
	}}
	pollRepo := &fakePollRepo{polls: map[string]*domainpolls.Poll{
		"poll-note": {NoteID: "poll-note", AuthorID: local.ID, Choices: []string{"cats", "dogs"}, Votes: []int{0, 0}},
	}}
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetPollRepository(pollRepo)
	result, err := h.performCreate(context.Background(), remote, map[string]any{
		"id": "https://remote.example/activities/vote", "type": "Create", "actor": remote.URI,
		"object": map[string]any{
			"id": "https://remote.example/votes/1", "type": "Note", "attributedTo": remote.URI,
			"to": []any{local.URI}, "inReplyTo": pollURI, "name": "dogs",
		},
	})
	if err != nil || result != "ok: poll vote created" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if pollRepo.polls["poll-note"].Votes[1] != 1 {
		t.Fatalf("poll votes = %#v", pollRepo.polls["poll-note"].Votes)
	}
	if noteRepo.notes["https://remote.example/votes/1"] != nil {
		t.Fatal("vote reply was stored as a regular Note")
	}
}

func TestPerformMoveValidatesAliasAndMigratesLocalFollowers(t *testing.T) {
	sourceHost := "old.example"
	destinationHost := "new.example"
	sourceURI := "https://old.example/users/alice"
	destinationURI := "https://new.example/users/alice"
	local := &actors.Actor{ID: "local-bob", URI: "https://rosmarinus.example/users/bob"}
	source := &actors.Actor{ID: "remote-source", URI: sourceURI, Host: &sourceHost}
	destination := &actors.Actor{ID: "remote-destination", URI: destinationURI, Host: &destinationHost}
	repo := &fakeRepo{
		local: local,
		remotes: map[string]*actors.Actor{
			sourceURI:      source,
			destinationURI: destination,
		},
	}
	followRepo := &fakeFollowRepo{}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		ID:          "follow-old",
		FollowerID:  local.ID,
		FolloweeID:  source.ID,
		FollowerURI: local.URI,
		FolloweeURI: source.URI,
		Status:      follows.StatusAccepted,
	})
	client := &fakeClient{objects: map[string]map[string]any{
		sourceURI: {
			"id":                sourceURI,
			"type":              "Person",
			"preferredUsername": "alice",
			"inbox":             sourceURI + "/inbox",
			"movedTo":           destinationURI,
		},
		destinationURI: {
			"id":                destinationURI,
			"type":              "Person",
			"preferredUsername": "alice",
			"inbox":             destinationURI + "/inbox",
			"alsoKnownAs":       []any{sourceURI},
		},
	}}
	q := &fakeQueue{}
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 11, Timeout: time.Minute},
	}, nil, repo, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, client, local)

	result, err := h.performActivity(context.Background(), source, map[string]any{
		"id":     "https://old.example/activities/move",
		"type":   "Move",
		"actor":  sourceURI,
		"object": sourceURI,
		"target": destinationURI,
	})
	if err != nil || result != "ok: actor moved followers=1" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	updatedSource := repo.remotes[sourceURI]
	if updatedSource.MovedToURI != destinationURI || updatedSource.MovedAt == nil {
		t.Fatalf("source move metadata = %+v", updatedSource)
	}
	newFollow, _ := followRepo.Find(context.Background(), local.ID, repo.remotes[destinationURI].ID)
	if newFollow == nil || newFollow.Status != follows.StatusPending {
		t.Fatalf("destination follow = %+v", newFollow)
	}
	if q.task.Type != queue.TaskDeliver {
		t.Fatalf("move did not enqueue follow: %+v", q.task)
	}
	deliver := q.task.Payload.(queue.DeliverPayload)
	if deliver.Object["type"] != "Follow" || deliver.Object["object"] != destinationURI {
		t.Fatalf("unexpected move follow activity: %+v", deliver.Object)
	}
}

func TestPerformMoveRejectsDestinationWithoutReciprocalAlias(t *testing.T) {
	sourceHost := "old.example"
	destinationHost := "new.example"
	sourceURI := "https://old.example/users/alice"
	destinationURI := "https://new.example/users/alice"
	source := &actors.Actor{ID: "remote-source", URI: sourceURI, Host: &sourceHost}
	repo := &fakeRepo{remotes: map[string]*actors.Actor{
		sourceURI:      source,
		destinationURI: {ID: "remote-destination", URI: destinationURI, Host: &destinationHost},
	}}
	client := &fakeClient{objects: map[string]map[string]any{
		sourceURI: {
			"id": sourceURI, "type": "Person", "preferredUsername": "alice",
			"inbox": sourceURI + "/inbox", "movedTo": destinationURI,
		},
		destinationURI: {
			"id": destinationURI, "type": "Person", "preferredUsername": "alice",
			"inbox": destinationURI + "/inbox",
		},
	}}
	q := &fakeQueue{}
	h := New(config.Config{}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, client, nil)
	result, err := h.performActivity(context.Background(), source, map[string]any{
		"id": "https://old.example/activities/move", "type": "Move", "actor": sourceURI, "target": destinationURI,
	})
	if err != nil || result != "skip: destination alsoKnownAs does not include source" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if q.task.Type != "" {
		t.Fatalf("unverified move enqueued delivery: %+v", q.task)
	}
}

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nexryai/rosmarinus/internal/account"
	apactors "github.com/nexryai/rosmarinus/internal/activitypub/actors"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func TestCreateActorDerivesOwnershipAndIdentity(t *testing.T) {
	repo := &fakeRepo{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	created, err := h.CreateActor(context.Background(), "account-1", connector.ActorCreateCommand{
		Username: "alice-work",
		Name:     "Alice Work",
		Type:     "Person",
	})
	if err != nil {
		t.Fatalf("CreateActor returned error: %v", err)
	}
	if created.ActorID == "" || created.Username != "alice-work" || created.URI != "https://rosmarinus.example/users/"+created.ActorID {
		t.Fatalf("unexpected result: %+v", created)
	}
	if _, err := bson.ObjectIDFromHex(created.ActorID); err != nil {
		t.Fatalf("local actor ID is not ObjectID hex: %q: %v", created.ActorID, err)
	}
	if repo.local == nil || repo.local.OwnerAccountID != "account-1" || repo.local.IsSystemActor || !repo.local.IsLocked || !repo.local.IsDiscoverable || repo.local.PublicKeyID != created.URI+"#main-key" {
		t.Fatalf("unexpected stored actor: %+v", repo.local)
	}
}

func TestCreateActorRejectsInvalidUsername(t *testing.T) {
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	if _, err := h.CreateActor(context.Background(), "account-1", connector.ActorCreateCommand{Username: ".bad"}); err == nil {
		t.Fatal("expected invalid username to fail")
	}
}

func TestCreateServiceActorStoresBotSemantics(t *testing.T) {
	repo := &fakeRepo{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	if _, err := h.CreateActor(context.Background(), "account-1", connector.ActorCreateCommand{Username: "service", Type: "Service"}); err != nil {
		t.Fatalf("CreateActor returned error: %v", err)
	}
	if repo.local == nil || !repo.local.IsBot || apactors.RenderLocalActor(config.Config{PublicURL: "https://rosmarinus.example"}, repo.local)["type"] != "Service" {
		t.Fatalf("service Actor did not preserve bot semantics: %+v", repo.local)
	}
}

func TestUpdateActorPersistsPatchRendersPersonAndFansOutToRemoteFollowers(t *testing.T) {
	remoteHost := "remote.example"
	blockedHost := "blocked.example"
	local := &actors.Actor{
		ID: "local-alice", OwnerAccountID: "account-1", Username: "alice", Type: "Person",
		URI: "https://rosmarinus.example/users/local-alice", Inbox: "https://rosmarinus.example/users/local-alice/inbox",
		SharedInbox: "https://rosmarinus.example/inbox", PublicKeyID: "https://rosmarinus.example/users/local-alice#main-key",
		PublicKeyPEM: "public", PrivateKeyPEM: "private", IsDiscoverable: true,
	}
	remoteShared := &actors.Actor{
		ID: "remote-shared", URI: "https://remote.example/users/shared", Host: &remoteHost,
		Inbox: "https://remote.example/users/shared/inbox", SharedInbox: "https://remote.example/inbox",
	}
	remoteIndividual := &actors.Actor{
		ID: "remote-individual", URI: "https://remote.example/users/individual", Host: &remoteHost,
		Inbox: "https://remote.example/users/individual/inbox",
	}
	inactive := &actors.Actor{
		ID: "remote-inactive", URI: "https://remote.example/users/inactive", Host: &remoteHost,
		Inbox: "https://remote.example/users/inactive/inbox", IsSuspended: true,
	}
	blocked := &actors.Actor{
		ID: "remote-blocked", URI: "https://blocked.example/users/blocked", Host: &blockedHost,
		Inbox: "https://blocked.example/users/blocked/inbox",
	}
	repo := &fakeRepo{local: local, remotes: map[string]*actors.Actor{
		remoteShared.URI: remoteShared, remoteIndividual.URI: remoteIndividual,
		inactive.URI: inactive, blocked.URI: blocked,
	}}
	followRepo := &fakeFollowRepo{}
	for _, follow := range []follows.Follow{
		{ID: "follow-shared", FollowerID: remoteShared.ID, FolloweeID: local.ID, FollowerHost: &remoteHost, FollowerInbox: remoteShared.Inbox, FollowerSharedInbox: remoteShared.SharedInbox, Status: follows.StatusAccepted},
		{ID: "follow-individual", FollowerID: remoteIndividual.ID, FolloweeID: local.ID, FollowerHost: &remoteHost, FollowerInbox: remoteIndividual.Inbox, Status: follows.StatusAccepted},
		{ID: "follow-inactive", FollowerID: inactive.ID, FolloweeID: local.ID, FollowerHost: &remoteHost, FollowerInbox: inactive.Inbox, Status: follows.StatusAccepted},
		{ID: "follow-blocked", FollowerID: blocked.ID, FolloweeID: local.ID, FollowerHost: &blockedHost, FollowerInbox: blocked.Inbox, Status: follows.StatusAccepted},
	} {
		if _, err := followRepo.Upsert(context.Background(), follow); err != nil {
			t.Fatal(err)
		}
	}
	blockRepo := &fakeBlockRepo{blocks: map[string]*blocks.Block{
		local.ID + "\x00" + blocked.ID: {BlockerID: local.ID, BlockeeID: blocked.ID},
	}}
	emojiRepo := &fakeEmojiRepo{emojis: map[string]*emojis.Emoji{
		"local-blob": {
			ID: "local-blob", Name: "blob", URI: "https://rosmarinus.example/emojis/blob",
			PublicURL: "https://rosmarinus.example/media/blob.webp", MediaType: "image/webp",
			UpdatedAt: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		},
	}}
	q := &fakeQueue{}
	h := New(config.Config{PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 9, Timeout: time.Minute}}, nil, repo, &fakeNoteRepo{}, followRepo, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetEmojiRepository(emojiRepo)
	patch := connector.ActorUpdateData{
		Name: "Alice Updated", Summary: "Hello **world**", URL: "https://rosmarinus.example/@alice",
		ProfileFields: []connector.ActorProfileFieldData{{Name: "Site", Value: "https://example.com"}}, Birthday: "2000-01-02",
		Location: "Somewhere", AvatarURL: "https://rosmarinus.example/avatar.png", BannerURL: "https://rosmarinus.example/banner.png",
		Tags: []string{"go"}, EmojiNames: []string{"blob"}, IsBot: true, IsCat: true, IsLocked: true,
		IsDiscoverable: false, Present: map[string]bool{},
	}
	for _, field := range []string{"name", "summary", "url", "profile_fields", "birthday", "location", "avatar_url", "banner_url", "tags", "emoji_names", "is_bot", "is_cat", "is_locked", "is_discoverable"} {
		patch.Present[field] = true
	}
	updated, err := h.UpdateActor(context.Background(), "account-1", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch})
	if err != nil {
		t.Fatalf("UpdateActor returned error: %v", err)
	}
	if updated.ActorID != local.ID || updated.URI != local.URI || len(updated.Fields) != 14 {
		t.Fatalf("updated result = %+v", updated)
	}
	if repo.local.Name != patch.Name || repo.local.Summary != patch.Summary || repo.local.URL != patch.URL || !repo.local.IsCat || repo.local.IsDiscoverable {
		t.Fatalf("patch not persisted: %+v", repo.local)
	}
	if len(q.tasks) != 2 {
		t.Fatalf("deliveries = %d, want shared inbox plus individual inbox: %+v", len(q.tasks), q.tasks)
	}
	seen := map[string]bool{}
	for _, task := range q.tasks {
		payload, ok := task.Payload.(queue.DeliverPayload)
		if !ok || payload.Object["type"] != "Update" || payload.To == "" {
			t.Fatalf("unexpected delivery: %+v", task)
		}
		seen[payload.To] = true
		person, ok := payload.Object["object"].(map[string]any)
		if !ok || person["type"] != "Service" || person["id"] != local.URI || person["name"] != patch.Name || person["summary"] == nil || person["url"] != patch.URL {
			t.Fatalf("unexpected Person object: %+v", payload.Object["object"])
		}
		if person["inbox"] != local.Inbox || person["isCat"] != true || person["discoverable"] != false {
			t.Fatalf("unexpected identity/profile fields: %+v", person)
		}
		tags, ok := person["tag"].([]any)
		if !ok || len(tags) != 2 {
			t.Fatalf("unexpected Actor tags: %#v", person["tag"])
		}
		emojiTag := tags[0].(map[string]any)
		icon := emojiTag["icon"].(map[string]any)
		if emojiTag["name"] != ":blob:" || icon["url"] != "https://rosmarinus.example/media/blob.webp" {
			t.Fatalf("unexpected Actor emoji tag: %#v", emojiTag)
		}
	}
	if !seen[remoteShared.SharedInbox] || !seen[remoteIndividual.Inbox] {
		t.Fatalf("unexpected destinations: %+v", seen)
	}
}

func TestUpdateActorHonorsExplicitNullAndRejectsUnownedActor(t *testing.T) {
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", Username: "alice", URI: "https://rosmarinus.example/users/local", Name: "Alice"}
	repo := &fakeRepo{local: local}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, repo, &fakeNoteRepo{}, nil, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, nil, &fakeClient{}, local)
	patch := connector.ActorUpdateData{
		Name: "", Summary: "", Tags: nil, IsCat: false,
		Present: map[string]bool{"name": true, "summary": true, "tags": true, "is_cat": true},
		Null:    map[string]bool{"name": true, "summary": true, "tags": true},
	}
	if _, err := h.UpdateActor(context.Background(), "account-1", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch}); err == nil {
		t.Fatal("expected missing follow/queue configuration to fail before mutation")
	}
	h.follows = &fakeFollowRepo{}
	h.queue = &fakeQueue{}
	if _, err := h.UpdateActor(context.Background(), "account-1", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch}); err != nil {
		t.Fatalf("UpdateActor returned error: %v", err)
	}
	if repo.local.Name != "" || repo.local.Summary != "" || repo.local.Tags != nil || repo.local.IsCat {
		t.Fatalf("explicit null patch was not applied: %+v", repo.local)
	}
	if _, err := h.UpdateActor(context.Background(), "other-account", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch}); err == nil {
		t.Fatal("unowned actor update was accepted")
	}
}

func TestUpdateActorRejectsDisablingMandatoryApproval(t *testing.T) {
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", Username: "alice", URI: "https://rosmarinus.example/users/local", IsLocked: true}
	repo := &fakeRepo{local: local}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	patch := connector.ActorUpdateData{IsLocked: false, Present: map[string]bool{"is_locked": true}}
	if _, err := h.UpdateActor(context.Background(), "account-1", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch}); err == nil {
		t.Fatal("accepted an update that disables mandatory approval")
	}
	if !repo.local.IsLocked {
		t.Fatal("invalid approval update mutated actor")
	}
}

func TestUpdateActorRejectsUnknownCustomEmojiBeforeMutation(t *testing.T) {
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", Username: "alice", URI: "https://rosmarinus.example/users/local", Name: "Alice"}
	repo := &fakeRepo{local: local}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	h.SetEmojiRepository(&fakeEmojiRepo{})
	patch := connector.ActorUpdateData{
		Name: "Mutated", EmojiNames: []string{"missing"},
		Present: map[string]bool{"name": true, "emoji_names": true},
	}
	if _, err := h.UpdateActor(context.Background(), "account-1", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch}); err == nil {
		t.Fatal("accepted an unknown local custom emoji")
	}
	if repo.local.Name != "Alice" {
		t.Fatalf("invalid emoji update mutated Actor: %+v", repo.local)
	}
}

func TestUpdateActorRejectsBotFlagForNonUserActorType(t *testing.T) {
	local := &actors.Actor{ID: "local", OwnerAccountID: "account-1", Username: "group", URI: "https://rosmarinus.example/users/local", Type: "Group"}
	repo := &fakeRepo{local: local}
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, repo, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
	patch := connector.ActorUpdateData{IsBot: true, Present: map[string]bool{"is_bot": true}}
	if _, err := h.UpdateActor(context.Background(), "account-1", connector.ActorUpdateCommand{ActorID: local.ID, Patch: patch}); err == nil {
		t.Fatal("accepted is_bot for a Group Actor")
	}
}

func TestDeleteActorSuspendsOwnedActorAndFansOutToKnownPeers(t *testing.T) {
	hostOne := "remote.example"
	hostTwo := "other.example"
	local := &actors.Actor{
		ID: "owned-actor", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/owned-actor",
		Username: "owned", Type: "Person", PrivateKeyPEM: "private",
	}
	remoteFollower := &actors.Actor{ID: "remote-follower", URI: "https://remote.example/users/follower", Host: &hostOne}
	remoteFollowing := &actors.Actor{ID: "remote-following", URI: "https://remote.example/users/following", Host: &hostOne}
	remoteIndividual := &actors.Actor{ID: "remote-individual", URI: "https://other.example/users/individual", Host: &hostTwo}
	repo := &fakeRepo{local: local, remotes: map[string]*actors.Actor{
		remoteFollower.URI: remoteFollower, remoteFollowing.URI: remoteFollowing, remoteIndividual.URI: remoteIndividual,
	}}
	followRepo := &fakeFollowRepo{follows: map[string]*follows.Follow{}}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		ID: "01", FollowerID: remoteFollower.ID, FolloweeID: local.ID, FollowerHost: &hostOne,
		FollowerSharedInbox: "https://remote.example/inbox", Status: follows.StatusAccepted,
	})
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		ID: "02", FollowerID: local.ID, FolloweeID: remoteFollowing.ID, FolloweeHost: &hostOne,
		FolloweeSharedInbox: "https://remote.example/inbox", Status: follows.StatusAccepted,
	})
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		ID: "03", FollowerID: local.ID, FolloweeID: remoteIndividual.ID, FolloweeHost: &hostTwo,
		FolloweeInbox: "https://other.example/users/individual/inbox", Status: follows.StatusAccepted,
	})
	queued := &fakeQueue{}
	h := New(config.Config{
		PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 11, Timeout: time.Minute},
	}, nil, repo, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, queued, &fakeClient{}, nil)

	deleted, err := h.DeleteActor(context.Background(), "account-1", connector.ActorDeleteCommand{ActorID: local.ID})
	if err != nil {
		t.Fatalf("DeleteActor returned error: %v", err)
	}
	if deleted.ActorID != local.ID || deleted.URI != local.URI || deleted.DeletedAt.IsZero() || !local.IsSuspended || local.DeletedAt == nil {
		t.Fatalf("deleted actor result=%+v actor=%+v", deleted, local)
	}
	if active, _ := repo.FindOwnedLocalByID(context.Background(), "account-1", local.ID); active != nil {
		t.Fatalf("deleted Actor remains active: %+v", active)
	}
	if len(queued.tasks) != 3 {
		t.Fatalf("queued tasks = %+v", queued.tasks)
	}
	destinations := map[string]queue.DeliverPayload{}
	for _, task := range queued.tasks[:2] {
		payload, ok := task.Payload.(queue.DeliverPayload)
		if !ok || payload.Object["id"] != local.URI+"#delete" || payload.Object["type"] != "Delete" || payload.Object["object"] != local.URI {
			t.Fatalf("unexpected Delete delivery: %+v", task)
		}
		destinations[payload.To] = payload
	}
	if len(destinations) != 2 || !destinations["https://remote.example/inbox"].IsSharedInbox || destinations["https://other.example/users/individual/inbox"].IsSharedInbox {
		t.Fatalf("delete destinations = %+v", destinations)
	}
	cleanupPayload, ok := queued.tasks[2].Payload.(queue.AccountDeletePayload)
	if !ok || !cleanupPayload.Local || cleanupPayload.ActorID != local.ID || cleanupPayload.ActorURI != local.URI {
		t.Fatalf("cleanup task = %+v", queued.tasks[2])
	}
}

func TestApplyAccountLifecycleFederatesSuspensionAndMatchingUndo(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{
		ID: "owned-actor", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/owned-actor",
		Username: "owned", Type: "Person", PrivateKeyPEM: "private",
	}
	remote := &actors.Actor{ID: "remote-follower", URI: "https://remote.example/users/follower", Host: &host}
	repo := &fakeRepo{local: local, remotes: map[string]*actors.Actor{remote.URI: remote}}
	followRepo := &fakeFollowRepo{follows: map[string]*follows.Follow{}}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		ID: "01", FollowerID: remote.ID, FolloweeID: local.ID, FollowerHost: &host,
		FollowerSharedInbox: "https://remote.example/inbox", Status: follows.StatusAccepted,
	})
	queued := &fakeQueue{}
	h := New(config.Config{DeliverQueue: config.QueueConfig{MaxRetry: 11, Timeout: time.Minute}}, nil, repo, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, queued, &fakeClient{}, nil)

	modified, err := h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusSuspended, false)
	if err != nil || modified != 1 || !local.IsSuspended || local.SuspendedAt == nil {
		t.Fatalf("suspend result modified=%d err=%v actor=%+v", modified, err, local)
	}
	if len(queued.tasks) != 1 {
		t.Fatalf("suspension deliveries = %+v", queued.tasks)
	}
	deletePayload := queued.tasks[0].Payload.(queue.DeliverPayload)
	deleteID, _ := deletePayload.Object["id"].(string)
	if deletePayload.Object["type"] != "Delete" || deletePayload.Object["object"] != local.URI || deleteID == local.URI+"#delete" {
		t.Fatalf("suspension delivery = %+v", deletePayload)
	}
	if repeated, err := h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusSuspended, false); err != nil || repeated != 0 || len(queued.tasks) != 1 {
		t.Fatalf("repeated suspension modified=%d err=%v tasks=%+v", repeated, err, queued.tasks)
	}

	modified, err = h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusActive, false)
	if err != nil || modified != 1 || local.IsSuspended || local.SuspendedAt != nil {
		t.Fatalf("resume result modified=%d err=%v actor=%+v", modified, err, local)
	}
	if len(queued.tasks) != 2 {
		t.Fatalf("lifecycle deliveries = %+v", queued.tasks)
	}
	undoPayload := queued.tasks[1].Payload.(queue.DeliverPayload)
	embedded, ok := undoPayload.Object["object"].(map[string]any)
	if undoPayload.Object["type"] != "Undo" || !ok || embedded["id"] != deleteID || embedded["type"] != "Delete" {
		t.Fatalf("unsuspension delivery = %+v", undoPayload)
	}
}

func TestApplyAccountLifecycleDeletesActorsForDeletedAccount(t *testing.T) {
	local := &actors.Actor{ID: "owned-actor", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/owned-actor"}
	queued := &fakeQueue{}
	h := New(config.Config{}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, queued, &fakeClient{}, nil)
	modified, err := h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusDeleted, true)
	if err != nil || modified != 1 || local.DeletedAt == nil {
		t.Fatalf("delete lifecycle modified=%d err=%v actor=%+v", modified, err, local)
	}
	if len(queued.tasks) != 1 || queued.tasks[0].Type != queue.TaskAccountDelete {
		t.Fatalf("cleanup tasks = %+v", queued.tasks)
	}
}

func TestApplyAccountLifecycleResumesLegacySuspensionWithoutUndo(t *testing.T) {
	local := &actors.Actor{
		ID: "legacy-suspended", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/legacy-suspended", IsSuspended: true,
	}
	queued := &fakeQueue{}
	h := New(config.Config{}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, queued, &fakeClient{}, nil)
	modified, err := h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusActive, false)
	if err != nil || modified != 1 || local.IsSuspended {
		t.Fatalf("legacy resume modified=%d err=%v actor=%+v", modified, err, local)
	}
	if len(queued.tasks) != 0 {
		t.Fatalf("legacy suspension produced unmatched Undo: %+v", queued.tasks)
	}
}

func TestApplyAccountLifecycleKeepsStateWhenDeliveryCannotBeQueued(t *testing.T) {
	host := "remote.example"
	local := &actors.Actor{ID: "owned", OwnerAccountID: "account-1", URI: "https://rosmarinus.example/users/owned"}
	remote := &actors.Actor{ID: "remote", URI: "https://remote.example/users/remote", Host: &host}
	repo := &fakeRepo{local: local, remotes: map[string]*actors.Actor{remote.URI: remote}}
	followRepo := &fakeFollowRepo{follows: map[string]*follows.Follow{}}
	_, _ = followRepo.Upsert(context.Background(), follows.Follow{
		ID: "01", FollowerID: remote.ID, FolloweeID: local.ID, FollowerHost: &host,
		FollowerSharedInbox: "https://remote.example/inbox", Status: follows.StatusAccepted,
	})
	queued := &fakeQueue{err: errors.New("queue unavailable")}
	h := New(config.Config{}, nil, repo, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, queued, &fakeClient{}, nil)
	if _, err := h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusSuspended, false); err == nil {
		t.Fatal("expected suspension queue failure")
	}
	if local.IsSuspended || local.SuspendedAt != nil {
		t.Fatalf("queue failure suspended Actor without durable delivery: %+v", local)
	}

	suspendedAt := time.Now().UTC()
	local.IsSuspended = true
	local.SuspendedAt = &suspendedAt
	if _, err := h.ApplyAccountLifecycle(context.Background(), "account-1", account.StatusActive, false); err == nil {
		t.Fatal("expected unsuspension queue failure")
	}
	if !local.IsSuspended || local.SuspendedAt == nil {
		t.Fatalf("queue failure resumed Actor without durable Undo: %+v", local)
	}
}

func TestHandleDeliverTaskAllowsOnlyActorDeleteForDeletedSigner(t *testing.T) {
	deletedAt := time.Now().UTC()
	local := &actors.Actor{
		ID: "deleted-actor", URI: "https://rosmarinus.example/users/deleted-actor", IsSuspended: true, DeletedAt: &deletedAt,
	}
	client := &fakeClient{}
	h := New(config.Config{}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	deletePayload, _ := json.Marshal(queue.DeliverPayload{
		Version: 1, ActorID: local.ID, To: "https://remote.example/inbox",
		Object: map[string]any{"id": local.URI + "#delete", "type": "Delete", "actor": local.URI, "object": local.URI},
	})
	if err := h.HandleDeliverTask(context.Background(), asynq.NewTask(queue.TaskDeliver, deletePayload)); err != nil {
		t.Fatalf("deleted Actor Delete delivery failed: %v", err)
	}
	createPayload, _ := json.Marshal(queue.DeliverPayload{
		Version: 1, ActorID: local.ID, To: "https://remote.example/inbox",
		Object: map[string]any{"id": local.URI + "#stale", "type": "Create", "actor": local.URI, "object": "https://rosmarinus.example/notes/stale"},
	})
	if err := h.HandleDeliverTask(context.Background(), asynq.NewTask(queue.TaskDeliver, createPayload)); err == nil {
		t.Fatal("stale non-Delete activity was delivered for deleted Actor")
	}
	if client.deliveries != 1 {
		t.Fatalf("delivery calls = %d", client.deliveries)
	}
}

func TestHandleDeliverTaskAllowsSuspensionDeleteButRejectsStaleActivity(t *testing.T) {
	suspendedAt := time.Now().UTC()
	local := &actors.Actor{ID: "suspended-actor", URI: "https://rosmarinus.example/users/suspended-actor", IsSuspended: true, SuspendedAt: &suspendedAt}
	client := &fakeClient{}
	h := New(config.Config{}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	deletePayload, _ := json.Marshal(queue.DeliverPayload{
		Version: 1, ActorID: local.ID, To: "https://remote.example/inbox", Object: apactors.RenderSuspension(local, suspendedAt),
	})
	if err := h.HandleDeliverTask(context.Background(), asynq.NewTask(queue.TaskDeliver, deletePayload)); err != nil {
		t.Fatalf("suspended Actor Delete delivery failed: %v", err)
	}
	createPayload, _ := json.Marshal(queue.DeliverPayload{
		Version: 1, ActorID: local.ID, To: "https://remote.example/inbox",
		Object: map[string]any{"id": local.URI + "#stale", "type": "Create", "actor": local.URI, "object": local.URI + "/notes/stale"},
	})
	if err := h.HandleDeliverTask(context.Background(), asynq.NewTask(queue.TaskDeliver, createPayload)); err == nil {
		t.Fatal("stale non-Delete activity was delivered for suspended Actor")
	}
	if client.deliveries != 1 {
		t.Fatalf("delivery calls = %d", client.deliveries)
	}
}

func TestHandleDeliverTaskRetriesMatchingUndoUntilActorResumes(t *testing.T) {
	suspendedAt := time.Now().UTC()
	local := &actors.Actor{ID: "suspended-actor", URI: "https://rosmarinus.example/users/suspended-actor", IsSuspended: true, SuspendedAt: &suspendedAt}
	client := &fakeClient{}
	h := New(config.Config{}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, client, nil)
	payload, _ := json.Marshal(queue.DeliverPayload{
		Version: 1, ActorID: local.ID, To: "https://remote.example/inbox",
		Object: apactors.RenderUnsuspension(local, suspendedAt, suspendedAt.Add(time.Minute)),
	})
	task := asynq.NewTask(queue.TaskDeliver, payload)
	err := h.HandleDeliverTask(context.Background(), task)
	if err == nil || errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("matching Undo before resume error = %v", err)
	}
	local.IsSuspended = false
	local.SuspendedAt = nil
	if err := h.HandleDeliverTask(context.Background(), task); err != nil {
		t.Fatalf("matching Undo after resume failed: %v", err)
	}
	if client.deliveries != 1 {
		t.Fatalf("delivery calls = %d", client.deliveries)
	}
}

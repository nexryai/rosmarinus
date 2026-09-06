package worker

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nexryai/rosmarinus/internal/account"
	apactors "github.com/nexryai/rosmarinus/internal/activitypub/actors"
	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/activities"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/cleanup"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/instances"
	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/domain/reports"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeRepo struct {
	local            *actors.Actor
	remote           *actors.Actor
	remotes          map[string]*actors.Actor
	deletedRemoteURI string
}

func (f *fakeRepo) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == id && !f.local.IsSuspended && f.local.DeletedAt == nil {
		return f.local, nil
	}
	return nil, nil
}

func (f *fakeRepo) FindAnyByID(_ context.Context, id string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == id {
		return f.local, nil
	}
	if f.remote != nil && f.remote.ID == id {
		return f.remote, nil
	}
	for _, actor := range f.remotes {
		if actor.ID == id {
			return actor, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) FindLocalForDeliveryByID(ctx context.Context, id string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == id && f.local.Host == nil {
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

func (f *fakeRepo) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == actorID && f.local.OwnerAccountID == accountID && !f.local.IsSuspended && f.local.DeletedAt == nil {
		return f.local, nil
	}
	return nil, nil
}

func (f *fakeRepo) FindOwnedLocalByIDIncludingDeleted(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == actorID && f.local.OwnerAccountID == accountID && f.local.Host == nil && !f.local.IsSystemActor {
		return f.local, nil
	}
	return nil, nil
}

func (f *fakeRepo) ListOwnedLocalActorsPage(_ context.Context, accountID, afterID string, limit int, includeDeleted bool) ([]actors.Actor, error) {
	if f.local == nil || f.local.OwnerAccountID != accountID || f.local.Host != nil || f.local.IsSystemActor || f.local.ID <= afterID || (!includeDeleted && f.local.DeletedAt != nil) || limit <= 0 {
		return nil, nil
	}
	return []actors.Actor{*f.local}, nil
}

func (f *fakeRepo) SetOwnedLocalActorSuspended(ctx context.Context, accountID, actorID string, suspended bool, changedAt time.Time) (*actors.Actor, error) {
	actor, _ := f.FindOwnedLocalByIDIncludingDeleted(ctx, accountID, actorID)
	if actor == nil || actor.DeletedAt != nil || actor.IsSuspended == suspended {
		return nil, nil
	}
	actor.IsSuspended = suspended
	if suspended {
		changedAt = changedAt.UTC()
		actor.SuspendedAt = &changedAt
	} else {
		actor.SuspendedAt = nil
	}
	return actor, nil
}

func (f *fakeRepo) MarkOwnedLocalActorDeleted(ctx context.Context, accountID, actorID string, deletedAt time.Time) (*actors.Actor, error) {
	actor, _ := f.FindOwnedLocalByIDIncludingDeleted(ctx, accountID, actorID)
	if actor == nil {
		return nil, nil
	}
	if actor.DeletedAt == nil {
		deletedAt = deletedAt.UTC()
		actor.DeletedAt = &deletedAt
		actor.IsSuspended = true
		actor.SuspendedAt = &deletedAt
	}
	return actor, nil
}

func (f *fakeRepo) CreateOwnedLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	f.local = &actor
	return f.local, nil
}

func (f *fakeRepo) UpdateOwnedLocalActor(ctx context.Context, accountID, actorID string, patch actors.ActorPatch) (*actors.Actor, error) {
	if f.local == nil || f.local.ID != actorID || f.local.OwnerAccountID != accountID || f.local.Host != nil || f.local.IsSystemActor || f.local.IsSuspended {
		return nil, nil
	}
	updated := patch.Apply(*f.local)
	f.local = &updated
	return f.local, nil
}

func (f *fakeRepo) ListOwnedAccountIDs(ctx context.Context) ([]string, error) {
	if f.local != nil && f.local.OwnerAccountID != "" {
		return []string{f.local.OwnerAccountID}, nil
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
	if f.remotes != nil {
		return f.remotes[uri], nil
	}
	return nil, nil
}

func (f *fakeRepo) FindAnyByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	return f.FindByURI(ctx, uri)
}

func (f *fakeRepo) FilterActiveRemoteIDs(_ context.Context, ids []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	for _, id := range ids {
		candidates := []*actors.Actor{f.remote}
		for _, actor := range f.remotes {
			candidates = append(candidates, actor)
		}
		for _, actor := range candidates {
			if actor != nil && actor.ID == id && actor.Host != nil && !actor.IsSuspended {
				result[id] = struct{}{}
				break
			}
		}
	}
	return result, nil
}

func (f *fakeRepo) FindByPublicKeyID(ctx context.Context, keyID string) (*actors.Actor, error) {
	if f.remote != nil && f.remote.PublicKeyID == keyID {
		return f.remote, nil
	}
	for _, actor := range f.remotes {
		if actor.PublicKeyID == keyID {
			return actor, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) UpsertRemoteActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	actor.LastFetchedAt = time.Now()
	if f.remotes != nil {
		if existing := f.remotes[actor.URI]; existing != nil && existing.ID != "" {
			actor.ID = existing.ID
		}
		copy := actor
		f.remotes[actor.URI] = &copy
		return &copy, nil
	}
	f.remote = &actor
	return f.remote, nil
}

func (f *fakeRepo) AddRemoteFeaturedNote(ctx context.Context, actorURI, noteID string, limit int) (*actors.Actor, error) {
	actor, err := f.FindAnyByURI(ctx, actorURI)
	if err != nil || actor == nil || actor.Host == nil {
		return actor, err
	}
	for _, existing := range actor.FeaturedNoteIDs {
		if existing == noteID {
			return actor, nil
		}
	}
	if len(actor.FeaturedNoteIDs) < limit {
		actor.FeaturedNoteIDs = append(actor.FeaturedNoteIDs, noteID)
	}
	return actor, nil
}

func (f *fakeRepo) RemoveRemoteFeaturedNote(ctx context.Context, actorURI, noteID string) (*actors.Actor, error) {
	actor, err := f.FindAnyByURI(ctx, actorURI)
	if err != nil || actor == nil || actor.Host == nil {
		return actor, err
	}
	featured := actor.FeaturedNoteIDs[:0]
	for _, existing := range actor.FeaturedNoteIDs {
		if existing != noteID {
			featured = append(featured, existing)
		}
	}
	actor.FeaturedNoteIDs = featured
	return actor, nil
}

func (f *fakeRepo) MarkRemoteActorDeleted(ctx context.Context, uri string) error {
	f.deletedRemoteURI = uri
	if f.remote != nil && f.remote.URI == uri {
		f.remote.IsSuspended = true
		now := time.Now().UTC()
		f.remote.DeletedAt = &now
	}
	return nil
}

type fakeQueue struct {
	task  queue.Task
	tasks []queue.Task
	err   error
}

type fakeMediaRepo struct {
	record *domainmedia.Media
	body   []byte
}

func (f *fakeMediaRepo) FindByID(_ context.Context, id string) (*domainmedia.Media, error) {
	if f.record != nil && f.record.ID == id {
		return f.record, nil
	}
	return nil, nil
}

func (f *fakeMediaRepo) UpsertPending(_ context.Context, originalURL, publicURL string) (*domainmedia.Media, error) {
	if f.record == nil {
		f.record = &domainmedia.Media{
			ID: domainmedia.IDForURL(originalURL), OriginalURL: originalURL,
			PublicURL: publicURL, State: domainmedia.StatePending,
		}
	}
	return f.record, nil
}

func (f *fakeMediaRepo) StoreBlob(_ context.Context, _ string, source io.Reader, _ string, _ int64, _ string) error {
	body, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	f.body = bytes.Clone(body)
	return nil
}

func (f *fakeMediaRepo) OpenBlob(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.body)), nil
}

func (f *fakeMediaRepo) MarkReady(_ context.Context, id, contentType string, size int64, digest string) (*domainmedia.Media, error) {
	if f.record == nil || f.record.ID != id {
		return nil, fmt.Errorf("media not found")
	}
	f.record.State = domainmedia.StateReady
	f.record.ContentType = contentType
	f.record.Size = size
	f.record.SHA256 = digest
	return f.record, nil
}

func (f *fakeMediaRepo) MarkFailed(_ context.Context, id, message string) error {
	if f.record != nil && f.record.ID == id {
		f.record.State = domainmedia.StateFailed
		f.record.Error = message
	}
	return nil
}

type fakeMediaFetcher struct {
	result mediafetch.Result
	err    error
}

func TestResolveLocalAttachmentsRequiresOwnedReadyMedia(t *testing.T) {
	repo := &fakeMediaRepo{record: &domainmedia.Media{
		ID: "media-1", OwnerActorID: "actor-1", Name: "photo.png", PublicURL: "https://example.test/media/media-1",
		ContentType: "image/png", Width: 1600, Height: 800, State: domainmedia.StateReady,
	}}
	handler := &Handler{media: repo}
	attachments, err := handler.resolveLocalAttachments(context.Background(), "actor-1", []string{"media-1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || attachments[0].URL != repo.record.PublicURL || attachments[0].Width != 1600 || !attachments[0].Sensitive {
		t.Fatalf("attachments = %+v", attachments)
	}
	if _, err := handler.resolveLocalAttachments(context.Background(), "actor-2", []string{"media-1"}, false); err == nil {
		t.Fatal("cross-Actor media attachment was accepted")
	}
}

func (f fakeMediaFetcher) Fetch(context.Context, string) (mediafetch.Result, error) {
	return f.result, f.err
}

func (f fakeMediaFetcher) ValidateURL(target *url.URL) error {
	return mediafetch.ValidateURL(target)
}

func (f *fakeQueue) Enqueue(ctx context.Context, task queue.Task) error {
	if f.err != nil {
		return f.err
	}
	f.task = task
	f.tasks = append(f.tasks, task)
	return nil
}

func TestScheduleAndFetchMedia(t *testing.T) {
	repo := &fakeMediaRepo{}
	queued := &fakeQueue{}
	h := &Handler{
		cfg: config.Config{PublicURL: "https://rosmarinus.example"}, queue: queued,
		media: repo, mediaFetcher: fakeMediaFetcher{result: mediafetch.Result{Body: []byte("cached"), ContentType: "image/png"}},
	}
	if err := h.ScheduleMedia(context.Background(), "https://remote.example/file.png"); err != nil {
		t.Fatalf("ScheduleMedia returned error: %v", err)
	}
	if queued.task.Type != queue.TaskMedia || repo.record.PublicURL != "https://rosmarinus.example/media/"+repo.record.ID {
		t.Fatalf("unexpected scheduled media: task=%+v media=%+v", queued.task, repo.record)
	}
	payload, err := json.Marshal(queued.task.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.HandleMediaFetchTask(context.Background(), asynq.NewTask(queue.TaskMedia, payload)); err != nil {
		t.Fatalf("HandleMediaFetchTask returned error: %v", err)
	}
	if repo.record.State != domainmedia.StateReady || string(repo.body) != "cached" || repo.record.SHA256 == "" {
		t.Fatalf("media was not cached: record=%+v body=%q", repo.record, repo.body)
	}
}

func TestScheduleMediaRejectsUnsafeURLAndSkipsReadyMedia(t *testing.T) {
	repo := &fakeMediaRepo{}
	queued := &fakeQueue{}
	h := &Handler{cfg: config.Config{PublicURL: "https://rosmarinus.example"}, queue: queued, media: repo, mediaFetcher: fakeMediaFetcher{}}
	if err := h.ScheduleMedia(context.Background(), "https://127.0.0.1/file.png"); err == nil {
		t.Fatal("unsafe URL was scheduled")
	}
	if len(queued.tasks) != 0 {
		t.Fatalf("unsafe task was enqueued: %+v", queued.tasks)
	}
	repo.record = &domainmedia.Media{
		ID:          domainmedia.IDForURL("https://remote.example/file.png"),
		OriginalURL: "https://remote.example/file.png", State: domainmedia.StateReady,
	}
	if err := h.ScheduleMedia(context.Background(), repo.record.OriginalURL); err != nil {
		t.Fatalf("ScheduleMedia returned error: %v", err)
	}
	if len(queued.tasks) != 0 {
		t.Fatalf("ready media was re-enqueued: %+v", queued.tasks)
	}
}

type fakeConnectorPublisher struct {
	post         *connector.PostCreated
	notification *connector.NotificationCreated
	requested    *connector.FollowApproval
	requestedErr error
	requestCalls int
	completed    *connector.FollowApproval
	rejected     *connector.FollowApproval
}

func (f *fakeConnectorPublisher) PublishNotificationCreated(_ context.Context, payload connector.NotificationCreated) error {
	f.notification = &payload
	return nil
}

func (f *fakeConnectorPublisher) PublishPostCreated(ctx context.Context, payload connector.PostCreated) error {
	_ = ctx
	f.post = &payload
	return nil
}

func (f *fakeConnectorPublisher) PublishFollowApprovalRequested(ctx context.Context, payload connector.FollowApproval) error {
	_ = ctx
	f.requestCalls++
	if f.requestedErr != nil {
		return f.requestedErr
	}
	f.requested = &payload
	return nil
}

func (f *fakeConnectorPublisher) PublishFollowApprovalCompleted(ctx context.Context, payload connector.FollowApproval) error {
	_ = ctx
	f.completed = &payload
	return nil
}

func (f *fakeConnectorPublisher) PublishFollowApprovalRejected(ctx context.Context, payload connector.FollowApproval) error {
	_ = ctx
	f.rejected = &payload
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

func (f *fakeFollowRepo) FindByRemoteActivityID(_ context.Context, remoteActivityID string) (*follows.Follow, error) {
	for _, follow := range f.follows {
		if follow.RemoteActivityID == remoteActivityID {
			return follow, nil
		}
	}
	return nil, nil
}

func (f *fakeFollowRepo) ListFollowers(ctx context.Context, followeeID string, limit int) ([]follows.Follow, error) {
	result := make([]follows.Follow, 0)
	for _, follow := range f.follows {
		if follow.FolloweeID == followeeID && follow.Status == follows.StatusAccepted {
			result = append(result, *follow)
		}
	}
	return result, nil
}

func (f *fakeFollowRepo) ListFollowersPage(ctx context.Context, followeeID, afterID string, limit int) ([]follows.Follow, error) {
	result, err := f.ListFollowers(ctx, followeeID, limit)
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	page := make([]follows.Follow, 0, limit)
	for _, follow := range result {
		if follow.ID > afterID {
			page = append(page, follow)
			if len(page) == limit {
				break
			}
		}
	}
	return page, nil
}

func (f *fakeFollowRepo) ListFollowingPage(ctx context.Context, followerID, afterID string, limit int) ([]follows.Follow, error) {
	result := make([]follows.Follow, 0)
	for _, follow := range f.follows {
		if follow.FollowerID == followerID && follow.Status == follows.StatusAccepted {
			result = append(result, *follow)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	page := make([]follows.Follow, 0, limit)
	for _, follow := range result {
		if follow.ID > afterID {
			page = append(page, follow)
			if len(page) == limit {
				break
			}
		}
	}
	return page, nil
}

func (f *fakeFollowRepo) Upsert(ctx context.Context, follow follows.Follow) (*follows.Follow, error) {
	if f.follows == nil {
		f.follows = map[string]*follows.Follow{}
	}
	key := follow.FollowerID + "\x00" + follow.FolloweeID
	if existing := f.follows[key]; existing != nil {
		if existing.Status == follows.StatusAccepted && follow.Status == follows.StatusPending {
			return existing, nil
		}
		if follow.Status != "" {
			existing.Status = follow.Status
		}
		return existing, nil
	}
	if follow.ID == "" {
		follow.ID = "follow-id"
	}
	if follow.Status == "" {
		follow.Status = follows.StatusAccepted
	}
	f.follows[key] = &follow
	return &follow, nil
}

func (f *fakeFollowRepo) Approve(ctx context.Context, followerID, followeeID string) (*follows.Follow, error) {
	existing, _ := f.Find(ctx, followerID, followeeID)
	if existing == nil {
		return nil, nil
	}
	existing.Status = follows.StatusAccepted
	now := time.Now().UTC()
	existing.AcceptedAt = &now
	return existing, nil
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

type fakeBlockRepo struct {
	blocks  map[string]*blocks.Block
	deleted *blocks.Block
}

func (f *fakeBlockRepo) Find(ctx context.Context, blockerID, blockeeID string) (*blocks.Block, error) {
	if f.blocks == nil {
		return nil, nil
	}
	return f.blocks[blockerID+"\x00"+blockeeID], nil
}

func (f *fakeBlockRepo) Upsert(ctx context.Context, block blocks.Block) (*blocks.Block, error) {
	if f.blocks == nil {
		f.blocks = map[string]*blocks.Block{}
	}
	key := block.BlockerID + "\x00" + block.BlockeeID
	if existing := f.blocks[key]; existing != nil {
		return existing, nil
	}
	if block.ID == "" {
		block.ID = "block-id"
	}
	f.blocks[key] = &block
	return &block, nil
}

func (f *fakeBlockRepo) Delete(ctx context.Context, blockerID, blockeeID, remoteUndoActivityID string) error {
	existing, _ := f.Find(ctx, blockerID, blockeeID)
	if existing != nil {
		copy := *existing
		copy.RemoteUndoActivityID = remoteUndoActivityID
		f.deleted = &copy
		delete(f.blocks, blockerID+"\x00"+blockeeID)
	}
	return nil
}

type fakeNoteRepo struct {
	notes map[string]*domainnotes.Note
}

type fakeNotificationRepo struct {
	notifications map[string]*notifications.Notification
}

type fakeEmojiRepo struct {
	emojis map[string]*emojis.Emoji
}

type fakePollRepo struct {
	polls  map[string]*domainpolls.Poll
	voters map[string]map[string]struct{}
}

type fakeAccountCleanupRepo struct {
	actorID    string
	noteID     string
	result     cleanup.Result
	noteResult cleanup.NoteResult
}

func (r *fakeAccountCleanupRepo) CleanupActor(_ context.Context, actorID string) (cleanup.Result, error) {
	r.actorID = actorID
	return r.result, nil
}

func (r *fakeAccountCleanupRepo) CleanupNote(_ context.Context, noteID string) (cleanup.NoteResult, error) {
	r.noteID = noteID
	return r.noteResult, nil
}

func (r *fakePollRepo) FindByNoteID(_ context.Context, noteID string) (*domainpolls.Poll, error) {
	return r.polls[noteID], nil
}

func (r *fakePollRepo) UpsertLocal(_ context.Context, poll domainpolls.Poll) (*domainpolls.Poll, error) {
	if r.polls == nil {
		r.polls = map[string]*domainpolls.Poll{}
	}
	copy := poll
	r.polls[poll.NoteID] = &copy
	return &copy, nil
}

func (r *fakePollRepo) UpsertRemote(_ context.Context, poll domainpolls.Poll) (*domainpolls.Poll, error) {
	if r.polls == nil {
		r.polls = map[string]*domainpolls.Poll{}
	}
	copy := poll
	r.polls[poll.NoteID] = &copy
	return &copy, nil
}

func (r *fakePollRepo) UpdateRemoteVotes(_ context.Context, noteID, authorID string, votes []int) (*domainpolls.Poll, error) {
	poll := r.polls[noteID]
	if poll == nil || poll.AuthorID != authorID {
		return nil, nil
	}
	poll.Votes = append([]int(nil), votes...)
	return poll, nil
}

func (r *fakePollRepo) RecordVote(_ context.Context, noteID, actorID string, choice int, createdAt time.Time) (*domainpolls.Vote, *domainpolls.Poll, error) {
	poll := r.polls[noteID]
	if poll == nil || choice < 0 || choice >= len(poll.Choices) {
		return nil, poll, domainpolls.ErrInvalidChoice
	}
	if poll.ExpiresAt != nil && !createdAt.Before(*poll.ExpiresAt) {
		return nil, poll, domainpolls.ErrExpired
	}
	poll.Votes[choice]++
	if r.voters == nil {
		r.voters = map[string]map[string]struct{}{}
	}
	if r.voters[noteID] == nil {
		r.voters[noteID] = map[string]struct{}{}
	}
	r.voters[noteID][actorID] = struct{}{}
	return &domainpolls.Vote{ID: "poll-vote-id", NoteID: noteID, ActorID: actorID, Choice: choice, CreatedAt: createdAt}, poll, nil
}

func (r *fakePollRepo) ListVoterActorIDs(_ context.Context, noteID string) ([]string, error) {
	ids := make([]string, 0, len(r.voters[noteID]))
	for actorID := range r.voters[noteID] {
		ids = append(ids, actorID)
	}
	return ids, nil
}

func (r *fakeEmojiRepo) UpsertRemote(_ context.Context, emoji emojis.Emoji) (*emojis.Emoji, error) {
	if r.emojis == nil {
		r.emojis = map[string]*emojis.Emoji{}
	}
	key := emoji.Host + "\x00" + emoji.Name
	copy := emoji
	copy.ID = "emoji-" + emoji.Name
	r.emojis[key] = &copy
	return &copy, nil
}

func (r *fakeEmojiRepo) UpsertLocal(_ context.Context, emoji emojis.Emoji) (*emojis.Emoji, error) {
	if r.emojis == nil {
		r.emojis = map[string]*emojis.Emoji{}
	}
	emoji.Host = ""
	copy := emoji
	r.emojis["local-"+emoji.Name] = &copy
	return &copy, nil
}

func (r *fakeEmojiRepo) FindLocalByName(_ context.Context, name string) (*emojis.Emoji, error) {
	for _, emoji := range r.emojis {
		if emoji.Host == "" && emoji.Name == strings.Trim(name, ":") {
			return emoji, nil
		}
	}
	return nil, nil
}

func (r *fakeEmojiRepo) FindLocalByNames(ctx context.Context, names []string) ([]emojis.Emoji, error) {
	result := make([]emojis.Emoji, 0, len(names))
	for _, name := range names {
		if emoji, _ := r.FindLocalByName(ctx, name); emoji != nil {
			result = append(result, *emoji)
		}
	}
	return result, nil
}

func (r *fakeNotificationRepo) Upsert(_ context.Context, notification notifications.Notification) (*notifications.Notification, error) {
	if r.notifications == nil {
		r.notifications = map[string]*notifications.Notification{}
	}
	key := notification.RecipientActorID + "\x00" + notification.Kind + "\x00" + notification.RemoteActivityID
	if existing := r.notifications[key]; existing != nil {
		return existing, nil
	}
	if notification.ID == "" {
		notification.ID = fmt.Sprintf("notification-%d", len(r.notifications)+1)
	}
	r.notifications[key] = &notification
	return &notification, nil
}

func (r *fakeNotificationRepo) MarkRead(_ context.Context, accountID, actorID, notificationID string) (*notifications.Notification, error) {
	for _, notification := range r.notifications {
		if notification.ID == notificationID && notification.RecipientAccountID == accountID && notification.RecipientActorID == actorID {
			notification.IsRead = true
			return notification, nil
		}
	}
	return nil, nil
}

func TestMarkNotificationReadScopesRecipientAccountAndActor(t *testing.T) {
	repo := &fakeNotificationRepo{notifications: map[string]*notifications.Notification{
		"key": {
			ID:                 "notification-1",
			RecipientAccountID: "account-1",
			RecipientActorID:   "actor-1",
		},
	}}
	h := &Handler{notifications: repo}
	if _, err := h.MarkNotificationRead(context.Background(), "account-1", "actor-2", "notification-1"); err == nil {
		t.Fatal("cross-Actor notification update succeeded")
	}
	result, err := h.MarkNotificationRead(context.Background(), "account-1", "actor-1", "notification-1")
	if err != nil {
		t.Fatalf("MarkNotificationRead returned error: %v", err)
	}
	if result.NotificationID != "notification-1" || !result.IsRead {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func (f *fakeNoteRepo) FindByID(ctx context.Context, id string) (*domainnotes.Note, error) {
	for _, note := range f.notes {
		if note.ID == id && note.DeletedAt == nil {
			return note, nil
		}
	}
	return nil, nil
}

func (f *fakeNoteRepo) FindAnyByID(ctx context.Context, id string) (*domainnotes.Note, error) {
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

func (f *fakeNoteRepo) FindAnyByURI(_ context.Context, uri string) (*domainnotes.Note, error) {
	if f.notes == nil {
		return nil, nil
	}
	note := f.notes[uri]
	if note == nil {
		for _, candidate := range f.notes {
			if candidate.URI == uri {
				return candidate, nil
			}
		}
	}
	return note, nil
}

func (f *fakeNoteRepo) ListActiveReferenceAuthorURIsPage(_ context.Context, noteID, afterURI string, limit int) ([]string, error) {
	seen := make(map[string]struct{})
	for _, note := range f.notes {
		if note == nil || note.DeletedAt != nil || note.AttributedTo == "" || (note.ReplyID != noteID && note.RenoteID != noteID && note.QuoteID != noteID) {
			continue
		}
		if note.AttributedTo > afterURI {
			seen[note.AttributedTo] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for uri := range seen {
		result = append(result, uri)
	}
	sort.Strings(result)
	if limit >= 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
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

func (f *fakeNoteRepo) CreateLocalNote(ctx context.Context, note domainnotes.Note) (*domainnotes.Note, error) {
	if f.notes == nil {
		f.notes = map[string]*domainnotes.Note{}
	}
	if note.CreatedAt.IsZero() {
		note.CreatedAt = time.Now().UTC()
	}
	if note.PublishedAt == nil {
		publishedAt := note.CreatedAt
		note.PublishedAt = &publishedAt
	}
	f.notes[note.ID] = &note
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

func (f *fakeNoteRepo) DeleteLocalNote(ctx context.Context, id, authorID string) error {
	if f.notes == nil {
		return nil
	}
	for _, note := range f.notes {
		if note.ID == id && note.AuthorID == authorID {
			now := time.Now().UTC()
			note.DeletedAt = &now
		}
	}
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

func (f *fakeReactionRepo) FindByID(ctx context.Context, id string) (*reactions.Reaction, error) {
	for _, reaction := range f.reactions {
		if reaction.ID == id {
			return reaction, nil
		}
	}
	return nil, nil
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

type fakeReportRepo struct {
	reports map[string]*reports.Report
}

func (f *fakeReportRepo) FindByRemoteActivityID(ctx context.Context, remoteActivityID string) (*reports.Report, error) {
	if f.reports == nil || remoteActivityID == "" {
		return nil, nil
	}
	return f.reports[remoteActivityID], nil
}

func (f *fakeReportRepo) Create(ctx context.Context, report reports.Report) (*reports.Report, error) {
	if f.reports == nil {
		f.reports = map[string]*reports.Report{}
	}
	if report.ID == "" {
		report.ID = "report-id"
	}
	f.reports[report.RemoteActivityID] = &report
	return &report, nil
}

type fakeClient struct {
	objects     map[string]map[string]any
	deliverErr  error
	deliveries  int
	lastDeliver string
}

type fakeWebFinger struct {
	query string
	uri   string
}

func (f *fakeWebFinger) ResolveActor(_ context.Context, query string) (string, error) {
	f.query = query
	return f.uri, nil
}

type fakeInstanceRepo struct {
	instance *instances.Instance
	received int
	success  int
	failure  int
	metadata instances.Metadata
}

func (r *fakeInstanceRepo) FindByHost(context.Context, string) (*instances.Instance, error) {
	return r.instance, nil
}

func (r *fakeInstanceRepo) Register(_ context.Context, host string, now time.Time) (*instances.Instance, bool, error) {
	if r.instance != nil {
		return r.instance, false, nil
	}
	r.instance = &instances.Instance{
		ID: "instance-id", Host: host, SuspensionState: instances.SuspensionNone,
		FirstRetrievedAt: now, UpdatedAt: now,
	}
	return r.instance, true, nil
}

func (r *fakeInstanceRepo) RecordReceived(_ context.Context, host string, now time.Time) (*instances.Instance, error) {
	instance, _, _ := r.Register(context.Background(), host, now)
	r.received++
	instance.LatestRequestReceivedAt = &now
	instance.IsNotResponding = false
	instance.NotRespondingSince = nil
	if instance.SuspensionState == instances.SuspensionAutoNotResponding {
		instance.SuspensionState = instances.SuspensionNone
	}
	return instance, nil
}

func (r *fakeInstanceRepo) RecordDeliverySuccess(_ context.Context, host string, now time.Time, status int) (*instances.Instance, error) {
	instance, _, _ := r.Register(context.Background(), host, now)
	r.success++
	instance.LatestRequestSentAt = &now
	instance.LatestStatus = status
	instance.IsNotResponding = false
	instance.NotRespondingSince = nil
	return instance, nil
}

func (r *fakeInstanceRepo) RecordDeliveryFailure(_ context.Context, host string, now time.Time, status int) (*instances.Instance, error) {
	instance, _, _ := r.Register(context.Background(), host, now)
	r.failure++
	instance.LatestRequestSentAt = &now
	instance.LatestStatus = status
	instance.IsNotResponding = true
	if instance.NotRespondingSince == nil {
		instance.NotRespondingSince = &now
	}
	return instance, nil
}

func (r *fakeInstanceRepo) UpdateMetadata(_ context.Context, host string, metadata instances.Metadata, now time.Time) (*instances.Instance, error) {
	instance, _, _ := r.Register(context.Background(), host, now)
	r.metadata = metadata
	instance.SoftwareName = strings.ToLower(metadata.SoftwareName)
	instance.SoftwareVersion = metadata.SoftwareVersion
	instance.Name = metadata.Name
	instance.IconURL = metadata.IconURL
	instance.FaviconURL = metadata.FaviconURL
	instance.InfoUpdatedAt = &now
	return instance, nil
}

func (r *fakeInstanceRepo) RefreshRelationshipCounts(_ context.Context, host string, now time.Time) (*instances.Instance, error) {
	instance, _, _ := r.Register(context.Background(), host, now)
	return instance, nil
}

func (r *fakeInstanceRepo) SuspendGone(_ context.Context, host string, now time.Time) (*instances.Instance, error) {
	instance, _, _ := r.Register(context.Background(), host, now)
	instance.SuspensionState = instances.SuspensionGone
	return instance, nil
}

type fakeInstanceMetadataFetcher struct {
	metadata instances.Metadata
	calls    int
}

func (f *fakeInstanceMetadataFetcher) Fetch(context.Context, string) (instances.Metadata, error) {
	f.calls++
	return f.metadata, nil
}

type deliveryStatusError struct {
	status int
}

func (e deliveryStatusError) Error() string {
	return fmt.Sprintf("delivery status %d", e.status)
}

func (e deliveryStatusError) HTTPStatusCode() int {
	return e.status
}

type fakeActivityLocker struct {
	acquired bool
	name     string
	unlocked bool
}

type fakeActivityReceiptRepo struct {
	mu        sync.Mutex
	completed map[string]string
	active    map[string]activities.Claim
	claims    int
	completes int
	releases  int
	lease     time.Duration
	retention time.Duration
}

func (r *fakeActivityReceiptRepo) Claim(_ context.Context, activityID, actorURI string, _ time.Time, lease, retention time.Duration) (*activities.Claim, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claims++
	r.lease = lease
	r.retention = retention
	if owner, ok := r.completed[activityID]; ok {
		if owner != actorURI {
			return nil, false, errors.New("activity id is already owned by a different actor")
		}
		return nil, false, nil
	}
	if _, ok := r.active[activityID]; ok {
		return nil, false, nil
	}
	if r.active == nil {
		r.active = map[string]activities.Claim{}
	}
	claim := activities.Claim{ActivityID: activityID, ActorURI: actorURI, Token: fmt.Sprintf("lease-%d", r.claims)}
	r.active[activityID] = claim
	return &claim, true, nil
}

func (r *fakeActivityReceiptRepo) Complete(_ context.Context, claim activities.Claim, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completes++
	if r.completed == nil {
		r.completed = map[string]string{}
	}
	r.completed[claim.ActivityID] = claim.ActorURI
	delete(r.active, claim.ActivityID)
	return nil
}

func (r *fakeActivityReceiptRepo) Release(_ context.Context, claim activities.Claim) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases++
	delete(r.active, claim.ActivityID)
	return nil
}

func (r *fakeActivityReceiptRepo) counts() (claims, completes, releases int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claims, r.completes, r.releases
}

func (l *fakeActivityLocker) Acquire(_ context.Context, name string) (func(context.Context) error, bool, error) {
	l.name = name
	if !l.acquired {
		return nil, false, nil
	}
	return func(context.Context) error {
		l.unlocked = true
		return nil
	}, true, nil
}

func (f *fakeClient) FetchObject(ctx context.Context, uri string, signer *actors.Actor) (map[string]any, error) {
	return f.objects[uri], nil
}

func (f *fakeClient) Deliver(ctx context.Context, target string, signer actors.Actor, object map[string]any) (int, error) {
	f.deliveries++
	f.lastDeliver = target
	if f.deliverErr != nil {
		var statusError interface{ HTTPStatusCode() int }
		if errors.As(f.deliverErr, &statusError) {
			return statusError.HTTPStatusCode(), f.deliverErr
		}
		return 0, f.deliverErr
	}
	return http.StatusAccepted, nil
}

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
	if post.ActorID != "relay" || post.NoteID != "note-1" || post.URI != "https://rosmarinus.example/notes/note-1" {
		t.Fatalf("unexpected post payload: %+v", post)
	}
	note, err := noteRepo.FindByID(context.Background(), "note-1")
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
	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "local-poll", Text: "choose",
		Poll: &connector.PollCreateCommand{Choices: []string{"cats", "dogs"}},
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	poll := pollRepo.polls["local-poll"]
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
	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
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
	note, err := noteRepo.FindByID(context.Background(), "specified-note")
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

	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "reply-and-quote", Text: "both",
		InReplyToURI: " " + replyTarget.URI + " ", QuoteURI: quoteTarget.URI,
		Visibility: string(domainnotes.VisibilityPublic),
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	stored, err := noteRepo.FindByID(context.Background(), "reply-and-quote")
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

	_, err := h.CreatePost(context.Background(), connector.PostCreateCommand{
		ActorID: local.ID, NoteID: "specified-reply", Text: "private reply",
		InReplyToURI: target.URI, Visibility: string(domainnotes.VisibilitySpecified),
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	stored, _ := noteRepo.FindByID(context.Background(), "specified-reply")
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
	h := New(config.Config{PublicURL: "https://rosmarinus.example", DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute}}, nil, &fakeRepo{local: local, remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetEmojiRepository(emojiRepo)

	created, err := h.CreateReaction(context.Background(), connector.ReactionCreateCommand{ActorID: local.ID, NoteID: "remote-note", Reaction: ":party:"})
	if err != nil {
		t.Fatalf("CreateReaction returned error: %v", err)
	}
	if created.Reaction != ":party@.:" {
		t.Fatalf("normalized reaction = %q", created.Reaction)
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
	noteRepo := &fakeNoteRepo{}
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{
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

func publicKeyPEM(key *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

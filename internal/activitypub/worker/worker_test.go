package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeRepo struct {
	local            *actors.Actor
	ownedLocals      []*actors.Actor
	remote           *actors.Actor
	remotes          map[string]*actors.Actor
	deletedRemoteURI string
}

func (f *fakeRepo) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == id && !f.local.IsSuspended && f.local.DeletedAt == nil {
		return f.local, nil
	}
	for _, actor := range f.ownedLocals {
		if actor != nil && actor.ID == id && actor.Host == nil && !actor.IsSuspended && actor.DeletedAt == nil {
			return actor, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) FindAnyByID(_ context.Context, id string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == id {
		return f.local, nil
	}
	for _, actor := range f.ownedLocals {
		if actor != nil && actor.ID == id {
			return actor, nil
		}
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
	for _, actor := range f.ownedLocals {
		if actor != nil && actor.ID == id && actor.Host == nil {
			return actor, nil
		}
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
	if len(f.ownedLocals) > 0 {
		result := make([]actors.Actor, 0, limit)
		for _, actor := range f.ownedLocals {
			if actor == nil || actor.OwnerAccountID != accountID || actor.Host != nil || actor.IsSystemActor || actor.ID <= afterID || (!includeDeleted && actor.DeletedAt != nil) {
				continue
			}
			result = append(result, *actor)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
		if len(result) > limit {
			result = result[:limit]
		}
		return result, nil
	}
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
	for _, actor := range f.ownedLocals {
		if actor != nil && actor.URI == uri {
			return actor, nil
		}
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
		const id = "507f1f77bcf86cd799439011"
		f.record = &domainmedia.Media{
			ID: id, OriginalURL: originalURL,
			PublicURL: strings.TrimRight(publicURL, "/") + "/" + id, State: domainmedia.StatePending,
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
		ID:          "507f1f77bcf86cd799439011",
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
	posts        []connector.PostCreated
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
	f.posts = append(f.posts, payload)
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

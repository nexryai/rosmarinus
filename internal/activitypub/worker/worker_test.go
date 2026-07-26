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
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/connector"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/domain/reports"
	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeRepo struct {
	local            *actors.Actor
	remote           *actors.Actor
	deletedRemoteURI string
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

func (f *fakeRepo) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	if f.local != nil && f.local.ID == actorID && f.local.OwnerAccountID == accountID && !f.local.IsSuspended {
		return f.local, nil
	}
	return nil, nil
}

func (f *fakeRepo) CreateOwnedLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	f.local = &actor
	return f.local, nil
}

func (f *fakeRepo) SuspendOwnedLocalActors(ctx context.Context, accountID string) (int64, error) {
	if f.local != nil && f.local.OwnerAccountID == accountID && !f.local.IsSystemActor && !f.local.IsSuspended {
		f.local.IsSuspended = true
		return 1, nil
	}
	return 0, nil
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

func (f *fakeRepo) MarkRemoteActorDeleted(ctx context.Context, uri string) error {
	f.deletedRemoteURI = uri
	if f.remote != nil && f.remote.URI == uri {
		f.remote.IsSuspended = true
	}
	return nil
}

type fakeQueue struct {
	task  queue.Task
	tasks []queue.Task
}

func (f *fakeQueue) Enqueue(ctx context.Context, task queue.Task) error {
	f.task = task
	f.tasks = append(f.tasks, task)
	return nil
}

type fakeConnectorPublisher struct {
	post      *connector.PostCreated
	requested *connector.FollowApproval
	completed *connector.FollowApproval
	rejected  *connector.FollowApproval
}

func (f *fakeConnectorPublisher) PublishPostCreated(ctx context.Context, payload connector.PostCreated) error {
	_ = ctx
	f.post = &payload
	return nil
}

func (f *fakeConnectorPublisher) PublishFollowApprovalRequested(ctx context.Context, payload connector.FollowApproval) error {
	_ = ctx
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
	objects map[string]map[string]any
}

func (f *fakeClient) FetchObject(ctx context.Context, uri string, signer *actors.Actor) (map[string]any, error) {
	return f.objects[uri], nil
}

func (f *fakeClient) Deliver(ctx context.Context, target string, signer actors.Actor, object map[string]any) error {
	return nil
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
	connectorPublisher := &fakeConnectorPublisher{}
	followsRepo := &fakeFollowRepo{}
	h := New(config.Config{
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, followsRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
	h.SetConnectorPublisher(connectorPublisher)
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

func TestProcessInboxAcceptWithoutIDMatchesLatestMisskey(t *testing.T) {
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
	if err != nil || result != "ok: outgoing follow accepted" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	follow, _ := followRepo.Find(context.Background(), local.ID, remote.ID)
	if follow == nil || follow.Status != follows.StatusAccepted {
		t.Fatalf("follow was not accepted: %+v", follow)
	}
}

func TestRejectOutgoingFollowRemovesRequest(t *testing.T) {
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
		"id":   "https://remote.example/rejects/1",
		"type": "Reject",
		"object": map[string]any{
			"id":     "https://rosmarinus.example/follows/local-alice/remote-bob",
			"type":   "Follow",
			"actor":  local.URI,
			"object": remote.URI,
		},
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
	h := New(config.Config{
		PublicURL:    "https://rosmarinus.example",
		DeliverQueue: config.QueueConfig{MaxRetry: 17, Timeout: time.Minute},
	}, nil, &fakeRepo{local: local}, noteRepo, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
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

func TestCreatePostPaginatesFollowerDeliveries(t *testing.T) {
	local := &actors.Actor{ID: "relay", URI: "https://rosmarinus.example/users/relay"}
	followRepo := &fakeFollowRepo{}
	for i := 0; i < postDeliveryFollowerLimit*2+5; i++ {
		id := fmt.Sprintf("follow-%03d", i)
		_, err := followRepo.Upsert(context.Background(), follows.Follow{
			ID:                  id,
			FollowerID:          "remote-" + id,
			FolloweeID:          local.ID,
			FollowerURI:         "https://remote.example/users/" + id,
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
	}, nil, &fakeRepo{local: local}, &fakeNoteRepo{}, followRepo, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, q, &fakeClient{}, local)
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
	if repo.local == nil || repo.local.OwnerAccountID != "account-1" || repo.local.IsSystemActor || repo.local.PublicKeyID != created.URI+"#main-key" {
		t.Fatalf("unexpected stored actor: %+v", repo.local)
	}
}

func TestCreateActorRejectsInvalidUsername(t *testing.T) {
	h := New(config.Config{PublicURL: "https://rosmarinus.example"}, nil, &fakeRepo{}, &fakeNoteRepo{}, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
	if _, err := h.CreateActor(context.Background(), "account-1", connector.ActorCreateCommand{Username: ".bad"}); err == nil {
		t.Fatal("expected invalid username to fail")
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
	h := New(config.Config{}, nil, &fakeRepo{local: local, remote: remote}, &fakeNoteRepo{}, &fakeFollowRepo{}, blockRepo, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, local)
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, &fakeReactionRepo{}, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
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
	h := New(config.Config{}, nil, &fakeRepo{remote: remote}, noteRepo, &fakeFollowRepo{}, &fakeBlockRepo{}, reactionRepo, &fakeReportRepo{}, &fakeQueue{}, &fakeClient{}, nil)
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
			"id":        "https://remote.example/activities/announce",
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
	announce := noteRepo.notes["https://remote.example/activities/announce"]
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

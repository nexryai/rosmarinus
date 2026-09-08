package worker

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nexryai/rosmarinus/internal/domain/activities"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/blocks"
	"github.com/nexryai/rosmarinus/internal/domain/cleanup"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/follows"
	"github.com/nexryai/rosmarinus/internal/domain/instances"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/notifications"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/domain/reactions"
	"github.com/nexryai/rosmarinus/internal/domain/reports"
)

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

func (f *fakeNoteRepo) NewID(context.Context) (string, error) {
	return bson.NewObjectID().Hex(), nil
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

func publicKeyPEM(key *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

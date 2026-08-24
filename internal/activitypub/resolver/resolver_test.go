package resolver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	domainemojis "github.com/nexryai/rosmarinus/internal/domain/emojis"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
)

func TestResolveActorHandle(t *testing.T) {
	repo := &resolverActorRepository{}
	resolver := NewWithWebFinger(repo, resolverFetcher{}, nil, resolverWebFinger{uri: "https://remote.example/users/alice"})

	actor, err := resolver.ResolveActorHandle(context.Background(), "alice@remote.example")
	if err != nil {
		t.Fatalf("ResolveActorHandle returned error: %v", err)
	}
	if actor.URI != "https://remote.example/users/alice" || repo.upserted == nil {
		t.Fatalf("actor was not resolved and stored: %+v", actor)
	}
}

func TestResolveActorHandleRequiresWebFinger(t *testing.T) {
	resolver := New(&resolverActorRepository{}, resolverFetcher{}, nil)
	if _, err := resolver.ResolveActorHandle(context.Background(), "alice@remote.example"); err == nil {
		t.Fatal("ResolveActorHandle should require WebFinger")
	}
}

func TestResolveActorUsesLocalRepositoryWithoutHTTP(t *testing.T) {
	local := &actors.Actor{ID: "local-alice", URI: "https://local.example/users/alice"}
	repo := &resolverActorRepository{existing: local}
	fetcher := &countingResolverFetcher{}
	resolver := New(repo, fetcher, nil)
	resolver.SetFederationPolicy(config.Config{PublicURL: "https://local.example"})

	resolved, err := resolver.ResolveActor(context.Background(), local.URI)
	if err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if resolved != local || fetcher.calls != 0 {
		t.Fatalf("resolved=%+v fetch calls=%d", resolved, fetcher.calls)
	}
}

func TestResolveActorDoesNotFetchMissingLocalActor(t *testing.T) {
	fetcher := &countingResolverFetcher{}
	resolver := New(&resolverActorRepository{}, fetcher, nil)
	resolver.SetFederationPolicy(config.Config{PublicURL: "https://local.example"})

	_, err := resolver.ResolveActor(context.Background(), "https://local.example/users/missing")
	if err == nil || !strings.Contains(err.Error(), "local actor not found") || fetcher.calls != 0 {
		t.Fatalf("err=%v fetch calls=%d", err, fetcher.calls)
	}
}

func TestResolveActorRejectsBlockedCachedActor(t *testing.T) {
	host := "social.blocked.example"
	repo := &resolverActorRepository{existing: &actors.Actor{ID: "remote", URI: "https://social.blocked.example/users/alice", Host: &host}}
	fetcher := &countingResolverFetcher{}
	resolver := New(repo, fetcher, nil)
	resolver.SetFederationPolicy(config.Config{FederationBlockedHosts: []string{"blocked.example"}})

	_, err := resolver.ResolveActor(context.Background(), repo.existing.URI)
	if err == nil || !strings.Contains(err.Error(), "blocked") || fetcher.calls != 0 {
		t.Fatalf("err=%v fetch calls=%d", err, fetcher.calls)
	}
}

func TestResolveActorRejectsFragment(t *testing.T) {
	fetcher := &countingResolverFetcher{}
	resolver := New(&resolverActorRepository{}, fetcher, nil)
	_, err := resolver.ResolveActor(context.Background(), "https://remote.example/users/alice#key")
	if err == nil || fetcher.calls != 0 {
		t.Fatalf("err=%v fetch calls=%d", err, fetcher.calls)
	}
}

func TestResolveActorRefreshesStaleRemoteActor(t *testing.T) {
	host := "remote.example"
	uri := "https://remote.example/users/alice"
	repo := &resolverActorRepository{existing: &actors.Actor{
		ID: "remote", URI: uri, Host: &host, LastFetchedAt: time.Now().Add(-25 * time.Hour),
	}}
	fetcher := &countingResolverFetcher{object: map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       uri, "type": "Person", "preferredUsername": "alice",
		"name": "Refreshed", "inbox": uri + "/inbox",
	}}
	resolver := New(repo, fetcher, nil)

	resolved, err := resolver.ResolveActor(context.Background(), uri)
	if err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if fetcher.calls != 1 || repo.upserted == nil || resolved.Name != "Refreshed" {
		t.Fatalf("resolved=%+v fetch calls=%d upserted=%+v", resolved, fetcher.calls, repo.upserted)
	}
}

func TestResolveActorUpsertsRemoteEmojiTags(t *testing.T) {
	uri := "https://remote.example/users/alice"
	fetcher := &countingResolverFetcher{object: map[string]any{
		"id": uri, "type": "Person", "preferredUsername": "alice", "inbox": uri + "/inbox",
		"tag": []any{map[string]any{
			"id": "https://remote.example/emojis/party", "type": "Emoji", "name": ":party:",
			"icon": map[string]any{"url": "https://remote.example/files/party.webp"},
		}},
	}}
	emojiRepo := &resolverEmojiRepository{}
	mediaScheduler := &resolverMediaScheduler{}
	resolver := New(&resolverActorRepository{}, fetcher, nil)
	resolver.SetEmojiRepository(emojiRepo)
	resolver.SetMediaScheduler(mediaScheduler)
	if _, err := resolver.ResolveActor(context.Background(), uri); err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if emojiRepo.upserted == nil || emojiRepo.upserted.Host != "remote.example" || emojiRepo.upserted.Name != "party" {
		t.Fatalf("emoji was not upserted: %+v", emojiRepo.upserted)
	}
	if len(mediaScheduler.urls) != 1 || mediaScheduler.urls[0] != "https://remote.example/files/party.webp" {
		t.Fatalf("emoji media was not scheduled: %v", mediaScheduler.urls)
	}
}

func TestResolveActorKeepsFreshRemoteActor(t *testing.T) {
	host := "remote.example"
	existing := &actors.Actor{ID: "remote", URI: "https://remote.example/users/alice", Host: &host, LastFetchedAt: time.Now()}
	fetcher := &countingResolverFetcher{}
	resolver := New(&resolverActorRepository{existing: existing}, fetcher, nil)

	resolved, err := resolver.ResolveActor(context.Background(), existing.URI)
	if err != nil || resolved != existing || fetcher.calls != 0 {
		t.Fatalf("resolved=%+v err=%v fetch calls=%d", resolved, err, fetcher.calls)
	}
}

func TestResolveActorUpdatesFeaturedNotes(t *testing.T) {
	actorURI := "https://remote.example/users/alice"
	featuredURI := "https://remote.example/users/alice/collections/featured"
	noteURI := "https://remote.example/notes/pinned"
	fetcher := &mappedResolverFetcher{objects: map[string]map[string]any{
		actorURI: {
			"id": actorURI, "type": "Person", "preferredUsername": "alice",
			"inbox": actorURI + "/inbox", "featured": featuredURI,
		},
		featuredURI: {
			"id": featuredURI, "type": "OrderedCollection", "orderedItems": []any{noteURI},
		},
		noteURI: remoteNoteObject(actorURI, "pinned", nil),
	}}
	repo := &resolverActorRepository{}
	noteRepo := &resolverNoteRepository{}
	locker := &resolverObjectLocker{}
	resolver := New(repo, fetcher, nil)
	resolver.SetNoteRepository(noteRepo)
	resolver.SetObjectLocker(locker)

	actor, err := resolver.ResolveActor(context.Background(), actorURI)
	if err != nil {
		t.Fatalf("ResolveActor returned error: %v", err)
	}
	if len(actor.FeaturedNoteIDs) != 1 || actor.FeaturedNoteIDs[0] == "" || len(noteRepo.notes) != 1 {
		t.Fatalf("actor=%+v notes=%+v", actor, noteRepo.notes)
	}
	if len(locker.names) != 2 || locker.unlocked != 2 {
		t.Fatalf("object locks names=%v unlocked=%d", locker.names, locker.unlocked)
	}
}

func TestResolveNoteResolvesReplyQuoteAndUsesObjectLocks(t *testing.T) {
	host := "remote.example"
	author := &actors.Actor{ID: "remote-author", URI: "https://remote.example/users/alice", Host: &host, LastFetchedAt: time.Now()}
	repo := &resolverActorRepository{existing: author}
	noteRepo := &resolverNoteRepository{}
	fetcher := &mappedResolverFetcher{objects: map[string]map[string]any{
		"https://remote.example/notes/root": remoteNoteObject(author.URI, "root", map[string]any{
			"inReplyTo": "https://remote.example/notes/reply",
			"quoteUrl":  "https://remote.example/notes/quote",
		}),
		"https://remote.example/notes/reply": remoteNoteObject(author.URI, "reply", nil),
		"https://remote.example/notes/quote": remoteNoteObject(author.URI, "quote", nil),
	}}
	locker := &resolverObjectLocker{}
	resolver := New(repo, fetcher, nil)
	resolver.SetNoteRepository(noteRepo)
	resolver.SetObjectLocker(locker)

	note, err := resolver.ResolveNote(context.Background(), "https://remote.example/notes/root")
	if err != nil {
		t.Fatalf("ResolveNote returned error: %v", err)
	}
	if note.ReplyID == "" || note.QuoteID == "" || len(noteRepo.notes) != 3 {
		t.Fatalf("note=%+v stored=%+v", note, noteRepo.notes)
	}
	if len(locker.names) != 3 || locker.unlocked != 3 {
		t.Fatalf("object locks names=%v unlocked=%d", locker.names, locker.unlocked)
	}
}

func TestResolveNoteRejectsRecursiveReferences(t *testing.T) {
	host := "remote.example"
	author := &actors.Actor{ID: "remote-author", URI: "https://remote.example/users/alice", Host: &host, LastFetchedAt: time.Now()}
	fetcher := &mappedResolverFetcher{objects: map[string]map[string]any{
		"https://remote.example/notes/a": remoteNoteObject(author.URI, "a", map[string]any{"inReplyTo": "https://remote.example/notes/b"}),
		"https://remote.example/notes/b": remoteNoteObject(author.URI, "b", map[string]any{"inReplyTo": "https://remote.example/notes/a"}),
	}}
	resolver := New(&resolverActorRepository{existing: author}, fetcher, nil)
	resolver.SetNoteRepository(&resolverNoteRepository{})

	_, err := resolver.ResolveNote(context.Background(), "https://remote.example/notes/a")
	if err == nil || !strings.Contains(err.Error(), "already resolved note") {
		t.Fatalf("expected recursive reference error, got %v", err)
	}
}

func TestResolveNoteIgnoresPermanentlyInvalidQuote(t *testing.T) {
	host := "remote.example"
	author := &actors.Actor{ID: "remote-author", URI: "https://remote.example/users/alice", Host: &host, LastFetchedAt: time.Now()}
	root := remoteNoteObject(author.URI, "root", map[string]any{"quoteUrl": "acct:invalid"})
	resolver := New(&resolverActorRepository{existing: author}, &mappedResolverFetcher{objects: map[string]map[string]any{
		"https://remote.example/notes/root": root,
	}}, nil)
	resolver.SetNoteRepository(&resolverNoteRepository{})

	note, err := resolver.ResolveNote(context.Background(), "https://remote.example/notes/root")
	if err != nil {
		t.Fatalf("ResolveNote returned error: %v", err)
	}
	if note.QuoteURI != "acct:invalid" || note.QuoteID != "" {
		t.Fatalf("unexpected quote resolution: %+v", note)
	}
}

func TestResolveNoteDoesNotFetchMissingLocalNote(t *testing.T) {
	fetcher := &mappedResolverFetcher{}
	resolver := New(&resolverActorRepository{}, fetcher, nil)
	resolver.SetFederationPolicy(config.Config{PublicURL: "https://local.example"})
	resolver.SetNoteRepository(&resolverNoteRepository{})

	_, err := resolver.ResolveNote(context.Background(), "https://local.example/notes/missing")
	if err == nil || !strings.Contains(err.Error(), "local note not found") || fetcher.calls != 0 {
		t.Fatalf("err=%v fetch calls=%d", err, fetcher.calls)
	}
}

func TestCurrentMisskeyMinimumActorAllowsMissingOutbox(t *testing.T) {
	host := "https://host1.test"
	preferredUsername := "AliceTest"
	actorID := host + "/users/alicetest"
	actor, err := ParseRemoteActor(map[string]any{
		"@context":          "https://www.w3.org/ns/activitystreams",
		"id":                actorID,
		"type":              "Person",
		"preferredUsername": preferredUsername,
		"inbox":             actorID + "/inbox",
	}, actorID)
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.URI != actorID {
		t.Fatalf("URI = %q", actor.URI)
	}
	if actor.Username != preferredUsername {
		t.Fatalf("Username = %q", actor.Username)
	}
	if actor.Inbox != actorID+"/inbox" {
		t.Fatalf("Inbox = %q", actor.Inbox)
	}
}

func TestParseRemoteActorRejectsWrongOutboxHostWhenPresent(t *testing.T) {
	_, err := ParseRemoteActor(map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://remote.example/users/alice/inbox",
		"outbox":            "https://evil.example/users/alice/outbox",
		"preferredUsername": "alice",
	}, "https://remote.example/users/alice")
	if err == nil {
		t.Fatal("ParseRemoteActor should reject a wrong outbox host")
	}
}

func TestParseRemoteActorIgnoresInvalidSharedInbox(t *testing.T) {
	actor, err := ParseRemoteActor(map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://remote.example/users/alice/inbox",
		"preferredUsername": "alice",
		"endpoints": map[string]any{
			"sharedInbox": "https://evil.example/inbox",
		},
	}, "https://remote.example/users/alice")
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.SharedInbox != "" || actor.Inbox != "https://remote.example/users/alice/inbox" {
		t.Fatalf("unexpected inboxes: %+v", actor)
	}
}

func TestConcordeTruncateLongActorName(t *testing.T) {
	host := "https://host1.test"
	actorID := host + "/users/alicetest"
	name := strings.Repeat("a", 129)
	actor, err := ParseRemoteActor(map[string]any{
		"@context":          "https://www.w3.org/ns/activitystreams",
		"id":                actorID,
		"type":              "Person",
		"preferredUsername": "alicetest",
		"name":              name,
		"inbox":             actorID + "/inbox",
		"outbox":            actorID + "/outbox",
	}, actorID)
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.Name != name[:128] {
		t.Fatalf("Name length = %d", len(actor.Name))
	}
}

func TestParseRemoteActor(t *testing.T) {
	actor, err := ParseRemoteActor(map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://remote.example/users/alice/inbox",
		"outbox":            "https://remote.example/users/alice/outbox",
		"followers":         "https://remote.example/users/alice/followers",
		"following":         map[string]any{"id": "https://remote.example/users/alice/following"},
		"featured":          "https://remote.example/users/alice/collections/featured",
		"movedTo":           "https://new.example/users/alice",
		"alsoKnownAs":       []any{"https://old.example/users/alice"},
		"preferredUsername": "alice",
		"name":              "Alice",
		"summary":           `<p>Hello <strong>world</strong> <a href="https://remote.example/tags/Go">#Go</a></p>`,
		"url":               map[string]any{"href": "https://remote.example/@alice"},
		"icon":              map[string]any{"url": "https://remote.example/files/avatar.webp"},
		"image":             []any{map[string]any{"url": "https://remote.example/files/banner.webp"}},
		"attachment": []any{map[string]any{
			"type": "PropertyValue", "name": "Site", "value": `<a href="https://example.com">Example</a>`,
		}},
		"tag": []any{
			map[string]any{"type": "Hashtag", "name": "#Go"},
			map[string]any{"type": "Emoji", "name": ":blob:", "icon": map[string]any{"url": "https://remote.example/files/blob.webp"}},
		},
		"vcard:bday":                "2000-01-02T00:00:00Z",
		"vcard:Address":             "Somewhere",
		"manuallyApprovesFollowers": true,
		"discoverable":              true,
		"isCat":                     true,
		"endpoints": map[string]any{
			"sharedInbox": "https://remote.example/inbox",
		},
		"publicKey": map[string]any{
			"id":           "https://remote.example/users/alice#main-key",
			"publicKeyPem": "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----\n",
		},
	}, "https://remote.example/users/alice")
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if actor.URI != "https://remote.example/users/alice" || actor.SharedInbox != "https://remote.example/inbox" {
		t.Fatalf("unexpected actor: %+v", actor)
	}
	if actor.FollowersURI != "https://remote.example/users/alice/followers" {
		t.Fatalf("FollowersURI = %q", actor.FollowersURI)
	}
	if actor.FollowingURI != "https://remote.example/users/alice/following" {
		t.Fatalf("FollowingURI = %q", actor.FollowingURI)
	}
	if actor.FeaturedURI != "https://remote.example/users/alice/collections/featured" {
		t.Fatalf("FeaturedURI = %q", actor.FeaturedURI)
	}
	if actor.MovedToURI != "https://new.example/users/alice" || len(actor.AlsoKnownAs) != 1 || actor.AlsoKnownAs[0] != "https://old.example/users/alice" {
		t.Fatalf("unexpected move metadata: %+v", actor)
	}
	if actor.Host == nil || *actor.Host != "remote.example" {
		t.Fatalf("unexpected host: %+v", actor.Host)
	}
	if actor.Summary != "Hello **world** #Go" || actor.URL != "https://remote.example/@alice" {
		t.Fatalf("unexpected profile summary/url: %+v", actor)
	}
	if len(actor.ProfileFields) != 1 || actor.ProfileFields[0].Value != "[Example](https://example.com)" {
		t.Fatalf("unexpected profile fields: %+v", actor.ProfileFields)
	}
	if actor.Birthday != "2000-01-02" || actor.Location != "Somewhere" || actor.AvatarURL == "" || actor.BannerURL == "" {
		t.Fatalf("unexpected actor profile metadata: %+v", actor)
	}
	if len(actor.Tags) != 1 || actor.Tags[0] != "go" || len(actor.EmojiNames) != 1 || actor.EmojiNames[0] != "blob" {
		t.Fatalf("unexpected actor tags/emojis: %+v", actor)
	}
	if actor.IsBot || !actor.IsCat || !actor.IsLocked || !actor.IsDiscoverable {
		t.Fatalf("unexpected actor flags: %+v", actor)
	}
}

func TestParseRemoteActorRejectsNonHTTPSProfileURL(t *testing.T) {
	_, err := ParseRemoteActor(map[string]any{
		"type": "Person", "id": "https://remote.example/users/alice",
		"inbox": "https://remote.example/users/alice/inbox", "preferredUsername": "alice",
		"url": "http://remote.example/@alice",
	}, "https://remote.example/users/alice")
	if err == nil || !strings.Contains(err.Error(), "actor url") {
		t.Fatalf("expected invalid profile URL error, got %v", err)
	}
}

func TestParseRemoteActorTruncatesUnicodeSummary(t *testing.T) {
	actor, err := ParseRemoteActor(map[string]any{
		"type": "Service", "id": "https://remote.example/users/bot",
		"inbox": "https://remote.example/users/bot/inbox", "preferredUsername": "bot",
		"summary": strings.Repeat("猫", maxActorSummaryLength+1),
	}, "https://remote.example/users/bot")
	if err != nil {
		t.Fatalf("ParseRemoteActor returned error: %v", err)
	}
	if len([]rune(actor.Summary)) != maxActorSummaryLength || !actor.IsBot {
		t.Fatalf("summary length=%d IsBot=%v", len([]rune(actor.Summary)), actor.IsBot)
	}
}

func TestParseRemoteActorRejectsWrongInboxHost(t *testing.T) {
	_, err := ParseRemoteActor(map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://evil.example/inbox",
		"outbox":            "https://remote.example/users/alice/outbox",
		"preferredUsername": "alice",
	}, "https://remote.example/users/alice")
	if err == nil {
		t.Fatalf("ParseRemoteActor should reject wrong inbox host")
	}
}

func TestParseRemoteActorRejectsWrongCollectionHosts(t *testing.T) {
	base := map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://remote.example/users/alice/inbox",
		"outbox":            "https://remote.example/users/alice/outbox",
		"preferredUsername": "alice",
	}
	for _, field := range []string{"followers", "following"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base)+1)
			for k, v := range base {
				object[k] = v
			}
			object[field] = "https://evil.example/users/alice/" + field
			_, err := ParseRemoteActor(object, "https://remote.example/users/alice")
			if err == nil {
				t.Fatalf("ParseRemoteActor should reject wrong %s host", field)
			}
		})
	}
}

type resolverActorRepository struct {
	upserted *actors.Actor
	existing *actors.Actor
}

type resolverEmojiRepository struct {
	upserted *domainemojis.Emoji
}

type resolverMediaScheduler struct {
	urls []string
}

func (s *resolverMediaScheduler) ScheduleMedia(_ context.Context, rawURL string) error {
	s.urls = append(s.urls, rawURL)
	return nil
}

func (r *resolverEmojiRepository) UpsertRemote(_ context.Context, emoji domainemojis.Emoji) (*domainemojis.Emoji, error) {
	r.upserted = &emoji
	return &emoji, nil
}

func (r *resolverActorRepository) FindLocalByID(context.Context, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *resolverActorRepository) FindLocalByUsername(context.Context, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *resolverActorRepository) FindOwnedLocalByID(context.Context, string, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *resolverActorRepository) CreateOwnedLocalActor(context.Context, actors.Actor) (*actors.Actor, error) {
	return nil, nil
}

func (r *resolverActorRepository) SuspendOwnedLocalActors(context.Context, string) (int64, error) {
	return 0, nil
}

func (r *resolverActorRepository) ListOwnedAccountIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (r *resolverActorRepository) FindByURI(context.Context, string) (*actors.Actor, error) {
	return r.existing, nil
}

func (r *resolverActorRepository) FindAnyByURI(context.Context, string) (*actors.Actor, error) {
	return r.existing, nil
}

func (r *resolverActorRepository) FilterActiveRemoteIDs(_ context.Context, ids []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if r.existing == nil || r.existing.Host == nil || r.existing.IsSuspended {
		return result, nil
	}
	for _, id := range ids {
		if id == r.existing.ID {
			result[id] = struct{}{}
		}
	}
	return result, nil
}

func (r *resolverActorRepository) FindByPublicKeyID(context.Context, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *resolverActorRepository) UpsertRemoteActor(_ context.Context, actor actors.Actor) (*actors.Actor, error) {
	actor.LastFetchedAt = time.Now()
	r.upserted = &actor
	r.existing = &actor
	return &actor, nil
}

func (r *resolverActorRepository) MarkRemoteActorDeleted(context.Context, string) error {
	return nil
}

type resolverFetcher struct{}

func (resolverFetcher) FetchObject(context.Context, string, *actors.Actor) (map[string]any, error) {
	return map[string]any{
		"type":              "Person",
		"id":                "https://remote.example/users/alice",
		"inbox":             "https://remote.example/users/alice/inbox",
		"outbox":            "https://remote.example/users/alice/outbox",
		"preferredUsername": "alice",
	}, nil
}

type countingResolverFetcher struct {
	calls  int
	object map[string]any
}

type mappedResolverFetcher struct {
	objects map[string]map[string]any
	calls   int
}

func (f *mappedResolverFetcher) FetchObject(_ context.Context, uri string, _ *actors.Actor) (map[string]any, error) {
	f.calls++
	return f.objects[uri], nil
}

type resolverNoteRepository struct {
	notes map[string]*domainnotes.Note
}

func (r *resolverNoteRepository) FindByID(_ context.Context, id string) (*domainnotes.Note, error) {
	for _, note := range r.notes {
		if note.ID == id {
			return note, nil
		}
	}
	return nil, nil
}

func (r *resolverNoteRepository) FindAnyByID(ctx context.Context, id string) (*domainnotes.Note, error) {
	return r.FindByID(ctx, id)
}

func (r *resolverNoteRepository) FindByURI(_ context.Context, uri string) (*domainnotes.Note, error) {
	return r.notes[uri], nil
}

func (r *resolverNoteRepository) CreateLocalNote(_ context.Context, note domainnotes.Note) (*domainnotes.Note, error) {
	return r.store(note), nil
}

func (r *resolverNoteRepository) UpsertRemoteNote(_ context.Context, note domainnotes.Note) (*domainnotes.Note, error) {
	if existing := r.notes[note.URI]; existing != nil {
		return existing, nil
	}
	return r.store(note), nil
}

func (r *resolverNoteRepository) DeleteRemoteNote(context.Context, string, string) error {
	return nil
}

func (r *resolverNoteRepository) DeleteLocalNote(context.Context, string, string) error {
	return nil
}

func (r *resolverNoteRepository) store(note domainnotes.Note) *domainnotes.Note {
	if r.notes == nil {
		r.notes = map[string]*domainnotes.Note{}
	}
	if note.ID == "" {
		note.ID = fmt.Sprintf("note-%d", len(r.notes)+1)
	}
	r.notes[note.URI] = &note
	return &note
}

type resolverObjectLocker struct {
	names    []string
	unlocked int
}

func (l *resolverObjectLocker) Acquire(_ context.Context, name string) (func(context.Context) error, bool, error) {
	l.names = append(l.names, name)
	return func(context.Context) error {
		l.unlocked++
		return nil
	}, true, nil
}

func remoteNoteObject(actorURI, id string, extra map[string]any) map[string]any {
	object := map[string]any{
		"id": "https://remote.example/notes/" + id, "type": "Note",
		"attributedTo": actorURI, "content": id,
		"to": "https://www.w3.org/ns/activitystreams#Public",
	}
	for key, value := range extra {
		object[key] = value
	}
	return object
}

func (f *countingResolverFetcher) FetchObject(context.Context, string, *actors.Actor) (map[string]any, error) {
	f.calls++
	return f.object, nil
}

type resolverWebFinger struct {
	uri string
}

func (w resolverWebFinger) ResolveActor(context.Context, string) (string, error) {
	return w.uri, nil
}

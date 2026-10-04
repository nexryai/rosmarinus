package mongostore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/readmodel"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type timelineMemoryStore struct {
	values         map[string][]byte
	getErr, setErr error
	ttl            time.Duration
}

func (s *timelineMemoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	value, ok := s.values[key]
	return value, ok, s.getErr
}
func (s *timelineMemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = value
	s.ttl = ttl
	return nil
}
func (s *timelineMemoryStore) Delete(_ context.Context, keys ...string) error {
	for _, key := range keys {
		delete(s.values, key)
	}
	return nil
}

type timelineQuery struct {
	filter bson.M
	limit  int
}
type timelineFixture struct {
	docs    []noteDocument
	hidden  map[string]bool
	queries []timelineQuery
}

func newTimelineFixture() *timelineFixture {
	f := &timelineFixture{hidden: map[string]bool{}}
	stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	for i := 0; i < 20; i++ {
		f.docs = append(f.docs, noteDocument{ID: fmt.Sprintf("%024x", 100-i), CreatedAt: stamp.Add(-time.Duration(i/2) * time.Second), AuthorID: "author", Visibility: "public", Text: "original"})
	}
	return f
}
func (f *timelineFixture) fetch(_ context.Context, filter bson.M, limit, direction int, _ []string) ([]noteDocument, error) {
	if direction != -1 {
		panic("unexpected direction")
	}
	f.queries = append(f.queries, timelineQuery{filter, limit})
	docs := make([]noteDocument, 0)
	for _, doc := range f.docs {
		if !f.hidden[doc.ID] && timelineMatches(filter, doc) {
			docs = append(docs, doc)
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		return cursorAtOrBefore(readmodel.Cursor{CreatedAt: docs[j].CreatedAt, ID: docs[j].ID}, readmodel.Cursor{CreatedAt: docs[i].CreatedAt, ID: docs[i].ID})
	})
	if len(docs) > limit {
		docs = docs[:limit]
	}
	return docs, nil
}
func timelineMatches(filter bson.M, doc noteDocument) bool {
	for key, value := range filter {
		switch key {
		case "$and":
			for _, sub := range value.(bson.A) {
				if !timelineMatches(sub.(bson.M), doc) {
					return false
				}
			}
		case "$or":
			matched := false
			for _, sub := range value.(bson.A) {
				matched = matched || timelineMatches(sub.(bson.M), doc)
			}
			if !matched {
				return false
			}
		case "createdAt":
			if stamp, ok := value.(time.Time); ok {
				if !doc.CreatedAt.Equal(stamp) {
					return false
				}
				continue
			}
			for op, v := range value.(bson.M) {
				stamp := v.(time.Time)
				if op == "$gte" && doc.CreatedAt.Before(stamp) || op == "$lt" && !doc.CreatedAt.Before(stamp) {
					return false
				}
			}
		case "_id":
			for op, v := range value.(bson.M) {
				switch op {
				case "$lt":
					if doc.ID >= v.(string) {
						return false
					}
				case "$in":
					found := false
					for _, id := range v.([]string) {
						found = found || doc.ID == id
					}
					if !found {
						return false
					}
				}
			}
		case "deletedAt":
			if doc.DeletedAt != nil {
				return false
			}
		case "visibility":
			if doc.Visibility != value.(string) {
				return false
			}
		case "authorId":
			if doc.AuthorID != value.(string) {
				return false
			}
		default:
			panic("unexpected filter " + key)
		}
	}
	return true
}
func timelineTestReader() (*SalviaReader, *timelineMemoryStore) {
	store := &timelineMemoryStore{values: map[string][]byte{}}
	return (&SalviaReader{}).WithTimelineCache(store, time.Minute, 8, 50*time.Millisecond), store
}
func timelineTestFilter() bson.M { return bson.M{"deletedAt": nil, "visibility": "public"} }
func timelineRead(t *testing.T, r *SalviaReader, f *timelineFixture, after readmodel.Cursor, limit int) []noteDocument {
	t.Helper()
	docs, err := r.timelineDocuments(context.Background(), "public", "viewer", timelineTestFilter(), after, limit, nil, f.fetch)
	if err != nil {
		t.Fatal(err)
	}
	return docs
}
func TestTimelineCacheReusesWindowWithBoundedQueriesAndCurrentContents(t *testing.T) {
	r, store := timelineTestReader()
	f := newTimelineFixture()
	cold := timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if len(f.queries) != 1 || f.queries[0].limit != 8 || len(store.values) != 1 || store.ttl != time.Minute {
		t.Fatal("cold read did not prefetch a bounded window")
	}
	f.docs[0].Text = "edited"
	f.queries = nil
	hot := timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if len(hot) != 3 || hot[0].ID != cold[0].ID || hot[0].Text != "edited" || len(f.queries) != 1 {
		t.Fatalf("hot read = %+v, queries=%d", hot, len(f.queries))
	}
	query, _ := json.Marshal(f.queries[0].filter)
	if !strings.Contains(string(query), "$gte") || !strings.Contains(string(query), "$in") {
		t.Fatalf("query was not bounded: %s", query)
	}

}
func TestTimelineCacheMatchesDurablePaginationAfterNewNotesAndDeletions(t *testing.T) {
	r, _ := timelineTestReader()
	f := newTimelineFixture()
	timelineRead(t, r, f, readmodel.Cursor{}, 3)
	f.docs = append(f.docs, noteDocument{ID: "new", CreatedAt: time.Now().UTC(), Visibility: "public", AuthorID: "author"})
	deleted := time.Now()
	f.docs[0].DeletedAt = &deleted
	for _, doc := range f.docs[1:7] {
		f.hidden[doc.ID] = true
	}
	for _, after := range []readmodel.Cursor{{}, {CreatedAt: f.docs[2].CreatedAt, ID: f.docs[2].ID}, {CreatedAt: f.docs[8].CreatedAt, ID: f.docs[8].ID}} {
		got := timelineRead(t, r, f, after, 4)
		want, err := f.fetch(context.Background(), withCreatedCursor(timelineTestFilter(), after), 4, -1, nil)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("after=%+v got=%+v want=%+v", after, got, want)
		}
	}
}
func TestTimelineCachePagesWithEqualTimestampsWithoutDuplicates(t *testing.T) {
	r, _ := timelineTestReader()
	f := newTimelineFixture()
	var after readmodel.Cursor
	seen := map[string]bool{}
	for {
		docs := timelineRead(t, r, f, after, 3)
		if len(docs) == 0 {
			break
		}
		want, _ := f.fetch(context.Background(), withCreatedCursor(timelineTestFilter(), after), 3, -1, nil)
		if !reflect.DeepEqual(docs, want) {
			t.Fatal("cached page differs from durable page")
		}
		for _, doc := range docs {
			if seen[doc.ID] {
				t.Fatal("duplicate " + doc.ID)
			}
			seen[doc.ID] = true
		}
		last := docs[len(docs)-1]
		after = readmodel.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(seen) != len(f.docs) {
		t.Fatalf("missing notes: %d of %d", len(seen), len(f.docs))
	}
}
func TestTimelineCacheFailuresAndLossFallBackToMongo(t *testing.T) {
	for _, mode := range []string{"loss", "get failure", "set failure", "corrupt", "invalid", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			r, store := timelineTestReader()
			f := newTimelineFixture()
			want := timelineRead(t, r, f, readmodel.Cursor{}, 3)
			switch mode {
			case "loss":
				store.values = map[string][]byte{}
			case "get failure":
				store.getErr = errors.New("redis unavailable")
			case "set failure":
				store.values = map[string][]byte{}
				store.setErr = errors.New("redis unavailable")
			case "corrupt":
				for key := range store.values {
					store.values[key] = []byte("{")
				}
			case "invalid":
				for key := range store.values {
					store.values[key] = []byte(`{"IDs":["wrong"]}`)
				}
			case "disabled":
				r.timelineCacheTTL = 0
			}
			f.queries = nil
			got := timelineRead(t, r, f, readmodel.Cursor{}, 3)
			if !reflect.DeepEqual(got, want) || len(f.queries) != 1 {
				t.Fatalf("fallback changed results: got=%+v queries=%d", got, len(f.queries))
			}
			if mode == "get failure" && f.queries[0].limit != 3 {
				t.Fatal("Redis failure triggered prefetch")
			}
		})
	}
}
func TestTimelineCacheFingerprintSeparatesAuthorizationAndActors(t *testing.T) {
	base := timelineTestFilter()
	key := timelineKey("public", "viewer", base, nil)
	for _, other := range []string{timelineKey("home", "viewer", base, nil), timelineKey("public", "other", base, nil), timelineKey("public", "viewer", bson.M{"authorId": "changed-following"}, nil), timelineKey("public", "viewer", base, []string{"muted"})} {
		if key == other {
			t.Fatal("authorization contexts share cache key")
		}
	}
	r, _ := timelineTestReader()
	f := newTimelineFixture()
	timelineRead(t, r, f, readmodel.Cursor{}, 3)
	f.docs[0].Visibility = "followers"
	got := timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if len(got) != 3 || got[0].ID == f.docs[0].ID {
		t.Fatal("cached IDs bypassed current visibility")
	}
	// Cache changes cannot hide a MongoDB error, including during refills.
	_, err := r.timelineDocuments(context.Background(), "public", "viewer", base, readmodel.Cursor{}, 3, nil, func(context.Context, bson.M, int, int, []string) ([]noteDocument, error) {
		return nil, errors.New("mongo unavailable")
	})
	if err == nil {
		t.Fatal("MongoDB error was swallowed")
	}
}

type slowTimelineStore struct{ timelineMemoryStore }

func (s *slowTimelineStore) Get(ctx context.Context, _ string) ([]byte, bool, error) {
	<-ctx.Done()
	return nil, false, ctx.Err()
}
func TestTimelineCacheDeadlineFallsBackWithoutCancelingMongoRead(t *testing.T) {
	r, _ := timelineTestReader()
	r.timelineCache = &slowTimelineStore{}
	r.timelineCacheTimeout = 5 * time.Millisecond
	f := newTimelineFixture()
	start := time.Now()
	docs, err := r.timelineDocuments(context.Background(), "public", "viewer", timelineTestFilter(), readmodel.Cursor{}, 3, nil, func(ctx context.Context, filter bson.M, limit, direction int, muted []string) ([]noteDocument, error) {
		if ctx.Err() != nil {
			t.Fatal("cache deadline canceled MongoDB context")
		}
		return f.fetch(ctx, filter, limit, direction, muted)
	})
	if err != nil || len(docs) != 3 || time.Since(start) > time.Second {
		t.Fatalf("cache timeout did not fall back: %v", err)
	}
}

func TestTimelineCacheTailIncludesExactTimestampBoundary(t *testing.T) {
	r, store := timelineTestReader()
	f := newTimelineFixture()
	timelineRead(t, r, f, readmodel.Cursor{}, 3)
	var window timelineWindow
	for _, raw := range store.values {
		if err := json.Unmarshal(raw, &window); err != nil {
			t.Fatal(err)
		}
	}
	f.docs = append(f.docs, noteDocument{ID: "boundary", CreatedAt: window.Upper, AuthorID: "author", Visibility: "public"})
	got := timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if len(got) != 3 || got[0].ID != "boundary" {
		t.Fatal("note at inclusive tail boundary disappeared")
	}
}

func TestTimelineCacheRecentOnlyFeedAvoidsRepeatedPrefetch(t *testing.T) {
	r, store := timelineTestReader()
	f := newTimelineFixture()
	for i := range f.docs {
		f.docs[i].CreatedAt = time.Now().UTC()
	}
	timelineRead(t, r, f, readmodel.Cursor{}, 3)
	f.queries = nil
	got := timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if len(got) != 3 || len(f.queries) != 1 || f.queries[0].limit != 3 || len(store.values) != 1 {
		t.Fatal("busy recent feed repeatedly prefetched uncachable candidates")
	}
}

func TestTimelineCacheSnapshotDoesNotRenewAndBackdatedInsertRecovers(t *testing.T) {
	r, store := timelineTestReader()
	f := newTimelineFixture()
	timelineRead(t, r, f, readmodel.Cursor{}, 3)
	key := timelineKey("public", "viewer", timelineTestFilter(), nil)
	snapshot := string(store.values[key])
	timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if string(store.values[key]) != snapshot {
		t.Fatal("hot read replaced snapshot")
	}
	f.docs = append(f.docs, noteDocument{ID: "backdated", CreatedAt: f.docs[0].CreatedAt.Add(time.Millisecond), Visibility: "public", AuthorID: "author"})
	// Backdated imports into an already cached interval are discovered when the
	// bounded TTL expires; a cache miss must reconstruct them from MongoDB.
	delete(store.values, key)
	got := timelineRead(t, r, f, readmodel.Cursor{}, 3)
	if len(got) != 3 || got[0].ID != "backdated" {
		t.Fatal("expired snapshot failed to discover durable backdated insertion")
	}
}

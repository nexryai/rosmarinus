package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/nexryai/rosmarinus/internal/cache"
	"github.com/nexryai/rosmarinus/internal/readmodel"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// WithTimelineCache installs an optional, disposable candidate cache. Current
// authorization, note contents and author state are always read from MongoDB.
func (r *SalviaReader) WithTimelineCache(store cache.ValueStore, ttl time.Duration, size int, timeout time.Duration) *SalviaReader {
	r.timelineCache, r.timelineCacheTTL, r.timelineCacheSize, r.timelineCacheTimeout = store, ttl, size, timeout
	return r
}

type timelineWindow struct {
	IDs   []string
	Upper time.Time
	Lower readmodel.Cursor
}

type timelineFetch func(context.Context, bson.M, int, int, []string) ([]noteDocument, error)

func (r *SalviaReader) listTimeline(ctx context.Context, kind, viewer string, filter bson.M, after readmodel.Cursor, limit int, muted []string) ([]readmodel.Note, error) {
	docs, err := r.timelineDocuments(ctx, kind, viewer, filter, after, limit, muted, r.listNoteDocuments)
	if err != nil {
		return nil, err
	}
	return r.enrichNotes(ctx, viewer, docs)
}

func timelineKey(kind, viewer string, filter bson.M, muted []string) string {
	// JSON sorts map keys; the fingerprint includes the current follow, block and
	// mute sets, including expired mutes, rather than trusting cached permissions.
	raw, _ := json.Marshal([]any{kind, viewer, filter, muted})
	sum := sha256.Sum256(raw)
	return "timeline:v1:" + hex.EncodeToString(sum[:])
}

func (r *SalviaReader) timelineDocuments(ctx context.Context, kind, viewer string, filter bson.M, after readmodel.Cursor, limit int, muted []string, fetch timelineFetch) ([]noteDocument, error) {
	requested := withCreatedCursor(filter, after)
	if r.timelineCache == nil || r.timelineCacheTTL <= 0 || r.timelineCacheTimeout <= 0 || r.timelineCacheSize < limit || limit <= 0 {
		return fetch(ctx, requested, limit, -1, muted)
	}
	key := timelineKey(kind, viewer, filter, muted)
	cacheCtx, cancel := context.WithTimeout(ctx, r.timelineCacheTimeout)
	raw, found, cacheErr := r.timelineCache.Get(cacheCtx, key)
	cancel()
	var window timelineWindow
	valid := found && cacheErr == nil && json.Unmarshal(raw, &window) == nil && validTimelineWindow(window, r.timelineCacheSize)
	if !valid {
		// A Redis failure must not trigger speculative prefetching or change the
		// durable read path. Historical pages do not evict the recent window.
		if cacheErr != nil || !after.CreatedAt.IsZero() {
			return fetch(ctx, requested, limit, -1, muted)
		}
		upper := time.Now().UTC().Add(-5 * time.Second).Truncate(time.Millisecond)
		docs, err := fetch(ctx, requested, r.timelineCacheSize, -1, muted)
		if err != nil {
			return nil, err
		}
		window = timelineWindow{Upper: upper}
		for _, doc := range docs {
			if doc.CreatedAt.Before(upper) {
				window.IDs = append(window.IDs, doc.ID)
				window.Lower = readmodel.Cursor{CreatedAt: doc.CreatedAt, ID: doc.ID}
			}
		}
		// An empty window memoizes feeds wholly inside the recent overlap, avoiding
		// repeated speculative prefetches on busy feeds where the ordinary query wins.
		payload, err := json.Marshal(window)
		if err == nil {
			cacheCtx, cancel = context.WithTimeout(ctx, r.timelineCacheTimeout)
			_ = r.timelineCache.Set(cacheCtx, key, payload, r.timelineCacheTTL)
			cancel()
		}
		if len(docs) > limit {
			docs = docs[:limit]
		}
		return docs, nil
	}
	if len(window.IDs) == 0 || (!after.CreatedAt.IsZero() && cursorAtOrBefore(after, window.Lower)) {
		return fetch(ctx, requested, limit, -1, muted)
	}
	// Overlap the recent tail by five seconds to cover same-millisecond IDs and
	// ordinary in-flight inserts. Never extend the snapshot's TTL on a cache hit.
	candidates := bson.M{"$or": bson.A{
		bson.M{"createdAt": bson.M{"$gte": window.Upper}},
		bson.M{"_id": bson.M{"$in": window.IDs}, "createdAt": bson.M{"$lt": window.Upper}},
	}}
	docs, err := fetch(ctx, andTimelineFilters(requested, candidates), limit, -1, muted)
	if err != nil || len(docs) >= limit {
		return docs, err
	}
	if len(docs) < limit {
		// Deleted, suspended or newly hidden candidates cannot shorten a page: fill
		// it from the indexed range older than the cached window, even if it was
		// originally exhausted (new durable data may have appeared meanwhile).
		older, err := fetch(ctx, withCreatedCursor(requested, window.Lower), limit-len(docs), -1, muted)
		if err != nil {
			return nil, err
		}
		docs = append(docs, older...)
	}
	return docs, nil
}

func andTimelineFilters(left, right bson.M) bson.M {
	return bson.M{"$and": bson.A{left, right}}
}

func cursorAtOrBefore(left, right readmodel.Cursor) bool {
	return left.CreatedAt.Before(right.CreatedAt) || (left.CreatedAt.Equal(right.CreatedAt) && left.ID <= right.ID)
}

func validTimelineWindow(window timelineWindow, size int) bool {
	if window.Upper.IsZero() {
		return false
	}
	if len(window.IDs) == 0 {
		return window.Lower.CreatedAt.IsZero() && window.Lower.ID == ""
	}
	if len(window.IDs) > size || window.Lower.CreatedAt.IsZero() || window.Lower.ID == "" || !window.Lower.CreatedAt.Before(window.Upper) {
		return false
	}
	seen := make(map[string]bool, len(window.IDs))
	for _, id := range window.IDs {
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	return seen[window.Lower.ID]
}

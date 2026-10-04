package mongostore

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/cache"
	"github.com/nexryai/rosmarinus/internal/readmodel"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// This isolated fixture exercises actual query plans and disposable Redis state;
// the real-Misskey fixture separately covers complete frontend projections.
func TestTimelineCacheMongoRedis(t *testing.T) {
	uri, addr := os.Getenv("TIMELINE_TEST_MONGO_URI"), os.Getenv("TIMELINE_TEST_REDIS_ADDR")
	if uri == "" || addr == "" {
		t.Skip("set TIMELINE_TEST_MONGO_URI and TIMELINE_TEST_REDIS_ADDR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("timeline_cache_test_" + bson.NewObjectID().Hex())
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(cleanup)
		_ = client.Disconnect(cleanup)
	})
	if err := BootstrapIndexes(ctx, db); err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: addr, ContextTimeoutEnabled: true, MaxRetries: -1, ReadTimeout: time.Second, WriteTimeout: time.Second})
	prefix := db.Name()
	t.Cleanup(func() {
		keys, _ := redisClient.Keys(context.Background(), prefix+":*").Result()
		if len(keys) > 0 {
			_ = redisClient.Del(context.Background(), keys...).Err()
		}
		_ = redisClient.Close()
	})
	store := cache.NewRedisValueStore(redisClient, prefix)
	reader := NewSalviaReader(db).WithTimelineCache(store, time.Minute, 128, time.Second)
	_, err = db.Collection("actors").InsertMany(ctx, []any{bson.M{"_id": "active", "deletedAt": nil}, bson.M{"_id": "suspended", "deletedAt": nil, "isSuspended": true}})
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	docs := make([]any, 0, 2128)
	for i := 0; i < 2128; i++ {
		author := "suspended"
		if i >= 2000 {
			author = "active"
		}
		docs = append(docs, noteDocument{ID: fmt.Sprintf("%024x", i+1), URI: fmt.Sprintf("https://peer.example/notes/%d", i+1), AuthorID: author, Visibility: "public", CreatedAt: stamp.Add(-time.Duration(i) * time.Millisecond)})
	}
	if _, err = db.Collection("notes").InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	filter := timelineTestFilter()
	readCached := func(after readmodel.Cursor) ([]noteDocument, error) {
		return reader.timelineDocuments(ctx, "public", "viewer", filter, after, 30, nil, reader.listNoteDocuments)
	}
	readDurable := func(after readmodel.Cursor) ([]noteDocument, error) {
		return reader.listNoteDocuments(ctx, withCreatedCursor(filter, after), 30, -1, nil)
	}
	if _, err = readCached(readmodel.Cursor{}); err != nil {
		t.Fatal(err)
	}
	key := timelineKey("public", "viewer", filter, nil)
	if _, found, err := store.Get(ctx, key); err != nil || !found {
		t.Fatalf("candidate cache missing: %v", err)
	}
	for _, after := range []readmodel.Cursor{{}, {CreatedAt: stamp.Add(-2014 * time.Millisecond), ID: fmt.Sprintf("%024x", 2015)}} {
		got, err := readCached(after)
		if err != nil {
			t.Fatal(err)
		}
		want, err := readDurable(after)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("cached query differs from durable query: %v", err)
		}
	}
	measure := func(read func(readmodel.Cursor) ([]noteDocument, error)) time.Duration {
		start := time.Now()
		for i := 0; i < 5; i++ {
			if _, err := read(readmodel.Cursor{}); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start) / 5
	}
	t.Logf("candidate queries with 2,000 suspended-author rows: MongoDB=%s Redis window=%s (excludes enrichment)", measure(readDurable), measure(readCached))
	if _, err := db.Collection("notes").UpdateOne(ctx, bson.M{"_id": fmt.Sprintf("%024x", 2001)}, bson.M{"$set": bson.M{"deletedAt": time.Now()}}); err != nil {
		t.Fatal(err)
	}
	got, err := readCached(readmodel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := readDurable(readmodel.Cursor{})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cached query retained a deleted Note: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	got, err = readCached(readmodel.Cursor{})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cache loss changed durable results: %v", err)
	}
}

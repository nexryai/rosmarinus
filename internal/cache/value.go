package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

type ValueStore interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, ...string) error
}

type RedisValueStore struct {
	client *redis.Client
	prefix string
}

func NewRedisValueStore(client *redis.Client, prefix string) *RedisValueStore {
	if prefix == "" {
		prefix = "rosmarinus:cache"
	}
	return &RedisValueStore{client: client, prefix: prefix}
}

func (s *RedisValueStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.Get(ctx, s.key(key)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (s *RedisValueStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, s.key(key), value, ttl).Err()
}

func (s *RedisValueStore) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	resolved := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != "" {
			resolved = append(resolved, s.key(key))
		}
	}
	if len(resolved) == 0 {
		return nil
	}
	return s.client.Del(ctx, resolved...).Err()
}

func (s *RedisValueStore) key(key string) string {
	return s.prefix + ":" + key
}

func cacheKey(namespace, value string) string {
	sum := sha256.Sum256([]byte(value))
	return namespace + ":" + hex.EncodeToString(sum[:])
}

func loadJSON[T any](ctx context.Context, store ValueStore, key string) (*T, bool) {
	raw, found, err := store.Get(ctx, key)
	if err != nil || !found {
		return nil, false
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		_ = store.Delete(ctx, key)
		return nil, false
	}
	return &value, true
}

func storeJSON(ctx context.Context, store ValueStore, key string, value any, ttl time.Duration) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = store.Set(ctx, key, raw, ttl)
}

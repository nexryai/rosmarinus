package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type LockStore interface {
	SetNX(context.Context, string, string, time.Duration) (bool, error)
	CompareAndDelete(context.Context, string, string) error
}

type RedisLockStore struct {
	client *redis.Client
}

func NewRedisLockStore(client *redis.Client) *RedisLockStore {
	return &RedisLockStore{client: client}
}

func (s *RedisLockStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, value, ttl).Result()
}

func (s *RedisLockStore) CompareAndDelete(ctx context.Context, key, value string) error {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`
	return s.client.Eval(ctx, script, []string{key}, value).Err()
}

type Locker struct {
	store  LockStore
	prefix string
	ttl    time.Duration
}

func NewLocker(store LockStore, prefix string, ttl time.Duration) *Locker {
	if prefix == "" {
		prefix = "rosmarinus:lock"
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Locker{store: store, prefix: prefix, ttl: ttl}
}

func (l *Locker) Acquire(ctx context.Context, name string) (func(context.Context) error, bool, error) {
	token, err := randomToken()
	if err != nil {
		return nil, false, err
	}
	key := l.prefix + ":" + name
	ok, err := l.store.SetNX(ctx, key, token, l.ttl)
	if err != nil || !ok {
		return nil, ok, err
	}
	unlock := func(ctx context.Context) error {
		return l.store.CompareAndDelete(ctx, key, token)
	}
	return unlock, true, nil
}

func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate lock token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

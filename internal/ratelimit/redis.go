package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Evaluator interface {
	Eval(context.Context, string, []string, ...interface{}) *redis.Cmd
}

type RedisLimiter struct {
	client Evaluator
}

const fixedWindowScript = `
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
if current > tonumber(ARGV[2]) then
  return {0, ttl}
end
return {1, ttl}
`

func NewRedisLimiter(client Evaluator) *RedisLimiter {
	return &RedisLimiter{client: client}
}

func (l *RedisLimiter) Allow(ctx context.Context, scope, identity string, limit int, window time.Duration) (bool, time.Duration, error) {
	if l == nil || l.client == nil {
		return false, 0, fmt.Errorf("rate limiter is not configured")
	}
	if scope == "" || identity == "" || limit <= 0 || window <= 0 {
		return false, 0, fmt.Errorf("rate limit scope, identity, limit, and window are required")
	}
	key := rateLimitKey(scope, identity)
	values, err := l.client.Eval(ctx, fixedWindowScript, []string{key}, window.Milliseconds(), limit).Slice()
	if err != nil {
		return false, 0, err
	}
	if len(values) != 2 {
		return false, 0, fmt.Errorf("unexpected rate-limit result")
	}
	allowed, ok := values[0].(int64)
	if !ok {
		return false, 0, fmt.Errorf("unexpected rate-limit decision type %T", values[0])
	}
	ttlMilliseconds, ok := values[1].(int64)
	if !ok {
		return false, 0, fmt.Errorf("unexpected rate-limit TTL type %T", values[1])
	}
	if ttlMilliseconds < 0 {
		ttlMilliseconds = window.Milliseconds()
	}
	return allowed == 1, time.Duration(ttlMilliseconds) * time.Millisecond, nil
}

func rateLimitKey(scope, identity string) string {
	digest := sha256.Sum256([]byte(scope + "\x00" + identity))
	return "rosmarinus:auth-rate:" + hex.EncodeToString(digest[:])
}

package ratelimit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeEvaluator struct {
	values []interface{}
}

func (e fakeEvaluator) Eval(ctx context.Context, _ string, _ []string, _ ...interface{}) *redis.Cmd {
	cmd := redis.NewCmd(ctx)
	cmd.SetVal(e.values)
	return cmd
}

func TestRateLimitKeyIsStableScopedAndOpaque(t *testing.T) {
	key := rateLimitKey("login.start", "192.0.2.1")
	if key != rateLimitKey("login.start", "192.0.2.1") {
		t.Fatal("rate limit key is not stable")
	}
	if key == rateLimitKey("login.finish", "192.0.2.1") || key == rateLimitKey("login.start", "192.0.2.2") {
		t.Fatal("rate limit key is not properly scoped")
	}
	if strings.Contains(key, "192.0.2.1") {
		t.Fatal("rate limit key exposes the client identity")
	}
}

func TestRedisLimiterReadsAtomicScriptDecision(t *testing.T) {
	limiter := NewRedisLimiter(fakeEvaluator{values: []interface{}{int64(0), int64(1500)}})
	allowed, retryAfter, err := limiter.Allow(context.Background(), "login.start", "192.0.2.1", 20, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if allowed || retryAfter != 1500*time.Millisecond {
		t.Fatalf("allowed=%v retryAfter=%s", allowed, retryAfter)
	}
}

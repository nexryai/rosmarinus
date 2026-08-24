package queue

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type TaskControls struct {
	Concurrency   int
	RatePerSecond int
}

type WorkerConfig struct {
	Concurrency int
	Tasks       map[string]TaskControls
}

type taskRateLimiter interface {
	Wait(context.Context, string, int) error
}

type redisTaskRateLimiter struct {
	client *redis.Client
	prefix string
}

func newRedisTaskRateLimiter(client *redis.Client, prefix string) *redisTaskRateLimiter {
	return &redisTaskRateLimiter{client: client, prefix: prefix}
}

func (l *redisTaskRateLimiter) Wait(ctx context.Context, taskType string, limit int) error {
	if limit <= 0 {
		return nil
	}
	for {
		delay, err := l.reserve(ctx, taskType, limit)
		if err != nil {
			return err
		}
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *redisTaskRateLimiter) reserve(ctx context.Context, taskType string, limit int) (time.Duration, error) {
	const script = `
local current = redis.call('TIME')
local key = KEYS[1] .. ':' .. current[1]
local count = redis.call('INCR', key)
if count == 1 then redis.call('EXPIRE', key, 2) end
if count <= tonumber(ARGV[1]) then return 0 end
return 1000 - math.floor(tonumber(current[2]) / 1000)
`
	key := l.prefix + ":" + cacheSafeTaskType(taskType)
	result, err := l.client.Eval(ctx, script, []string{key}, limit).Int64()
	if err != nil {
		return 0, fmt.Errorf("reserve queue rate limit: %w", err)
	}
	return time.Duration(result) * time.Millisecond, nil
}

func cacheSafeTaskType(taskType string) string {
	return strconv.FormatUint(uint64(fnv1a(taskType)), 16)
}

func fnv1a(value string) uint32 {
	const (
		offset = uint32(2166136261)
		prime  = uint32(16777619)
	)
	hash := offset
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= prime
	}
	return hash
}

func taskControlsMiddleware(limiter taskRateLimiter, controls map[string]TaskControls) asynq.MiddlewareFunc {
	semaphores := make(map[string]chan struct{}, len(controls))
	for taskType, control := range controls {
		if control.Concurrency > 0 {
			semaphores[taskType] = make(chan struct{}, control.Concurrency)
		}
	}
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			control, configured := controls[task.Type()]
			if !configured {
				return next.ProcessTask(ctx, task)
			}
			if limiter != nil && control.RatePerSecond > 0 {
				if err := limiter.Wait(ctx, task.Type(), control.RatePerSecond); err != nil {
					return err
				}
			}
			semaphore := semaphores[task.Type()]
			if semaphore != nil {
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return next.ProcessTask(ctx, task)
		})
	}
}

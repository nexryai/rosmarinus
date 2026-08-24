package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type recordingRateLimiter struct {
	calls atomic.Int32
	err   error
}

func (l *recordingRateLimiter) Wait(context.Context, string, int) error {
	l.calls.Add(1)
	return l.err
}

func TestTaskControlsMiddlewareAppliesRateAndConcurrency(t *testing.T) {
	limiter := &recordingRateLimiter{}
	middleware := taskControlsMiddleware(limiter, map[string]TaskControls{
		TaskInbox: {Concurrency: 2, RatePerSecond: 32},
	})
	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	handler := middleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		return nil
	}))

	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := handler.ProcessTask(context.Background(), asynq.NewTask(TaskInbox, nil)); err != nil {
				t.Errorf("ProcessTask returned error: %v", err)
			}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("configured tasks did not enter handler")
		}
	}
	select {
	case <-entered:
		t.Fatal("concurrency limit allowed a third task")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	group.Wait()
	if maximum.Load() != 2 || limiter.calls.Load() != 4 {
		t.Fatalf("maximum=%d rate_calls=%d", maximum.Load(), limiter.calls.Load())
	}
}

func TestTaskControlsMiddlewareStopsOnRateLimiterError(t *testing.T) {
	want := errors.New("redis unavailable")
	limiter := &recordingRateLimiter{err: want}
	called := false
	handler := taskControlsMiddleware(limiter, map[string]TaskControls{
		TaskDeliver: {Concurrency: 1, RatePerSecond: 1},
	})(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		called = true
		return nil
	}))
	if err := handler.ProcessTask(context.Background(), asynq.NewTask(TaskDeliver, nil)); !errors.Is(err, want) {
		t.Fatalf("ProcessTask error = %v", err)
	}
	if called {
		t.Fatal("handler ran after rate limiter failure")
	}
}

func TestUnconfiguredTaskBypassesControls(t *testing.T) {
	limiter := &recordingRateLimiter{err: errors.New("should not run")}
	called := false
	handler := taskControlsMiddleware(limiter, map[string]TaskControls{
		TaskInbox: {Concurrency: 1, RatePerSecond: 1},
	})(asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		called = true
		return nil
	}))
	if err := handler.ProcessTask(context.Background(), asynq.NewTask(TaskMedia, nil)); err != nil || !called {
		t.Fatalf("unconfigured task err=%v called=%v", err, called)
	}
}

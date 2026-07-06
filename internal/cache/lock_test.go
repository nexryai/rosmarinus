package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeLockStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newFakeLockStore() *fakeLockStore {
	return &fakeLockStore{values: map[string]string{}}
}

func (s *fakeLockStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	_ = ctx
	_ = ttl
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[key]; ok {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}

func (s *fakeLockStore) CompareAndDelete(ctx context.Context, key, value string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values[key] == value {
		delete(s.values, key)
	}
	return nil
}

func TestLockerAcquireAndRelease(t *testing.T) {
	store := newFakeLockStore()
	locker := NewLocker(store, "test", time.Minute)
	unlock, ok, err := locker.Acquire(context.Background(), "ap:https://example.test/notes/1")
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if !ok {
		t.Fatalf("first Acquire should succeed")
	}
	if _, ok, err := locker.Acquire(context.Background(), "ap:https://example.test/notes/1"); err != nil || ok {
		t.Fatalf("second Acquire ok=%v err=%v, want false nil", ok, err)
	}
	if err := unlock(context.Background()); err != nil {
		t.Fatalf("unlock returned error: %v", err)
	}
	if _, ok, err := locker.Acquire(context.Background(), "ap:https://example.test/notes/1"); err != nil || !ok {
		t.Fatalf("Acquire after unlock ok=%v err=%v, want true nil", ok, err)
	}
}

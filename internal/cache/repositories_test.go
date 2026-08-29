package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/instances"
)

type memoryValue struct {
	value     []byte
	expiresAt time.Time
}

type memoryValueStore struct {
	mu     sync.Mutex
	values map[string]memoryValue
}

func newMemoryValueStore() *memoryValueStore {
	return &memoryValueStore{values: map[string]memoryValue{}}
}

func (s *memoryValueStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, found := s.values[key]
	if !found || time.Now().After(value.expiresAt) {
		delete(s.values, key)
		return nil, false, nil
	}
	return append([]byte(nil), value.value...), true, nil
}

func (s *memoryValueStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = memoryValue{value: append([]byte(nil), value...), expiresAt: time.Now().Add(ttl)}
	return nil
}

func (s *memoryValueStore) Delete(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.values, key)
	}
	return nil
}

type cachedActorBacking struct {
	actor    *actors.Actor
	uriReads int
	keyReads int
}

func (r *cachedActorBacking) FindByID(context.Context, string) (*actors.Actor, error) {
	return r.actor, nil
}

func (r *cachedActorBacking) FindLocalByID(context.Context, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *cachedActorBacking) FindLocalByUsername(context.Context, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *cachedActorBacking) FindOwnedLocalByID(context.Context, string, string) (*actors.Actor, error) {
	return nil, nil
}

func (r *cachedActorBacking) FindOwnedLocalByIDIncludingDeleted(context.Context, string, string) (*actors.Actor, error) {
	return r.actor, nil
}

func (r *cachedActorBacking) ListOwnedLocalActorsPage(context.Context, string, string, int, bool) ([]actors.Actor, error) {
	return nil, nil
}

func (r *cachedActorBacking) FindLocalForDeliveryByID(context.Context, string) (*actors.Actor, error) {
	return r.actor, nil
}

func (r *cachedActorBacking) CreateOwnedLocalActor(_ context.Context, actor actors.Actor) (*actors.Actor, error) {
	r.actor = &actor
	return r.actor, nil
}

func (r *cachedActorBacking) UpdateOwnedLocalActor(_ context.Context, accountID, actorID string, patch actors.ActorPatch) (*actors.Actor, error) {
	if r.actor == nil || r.actor.ID != actorID || r.actor.OwnerAccountID != accountID || r.actor.IsSystemActor || r.actor.IsSuspended {
		return nil, nil
	}
	updated := patch.Apply(*r.actor)
	r.actor = &updated
	return r.actor, nil
}

func (r *cachedActorBacking) SetOwnedLocalActorSuspended(context.Context, string, string, bool, time.Time) (*actors.Actor, error) {
	return nil, nil
}

func (r *cachedActorBacking) MarkOwnedLocalActorDeleted(_ context.Context, accountID, actorID string, deletedAt time.Time) (*actors.Actor, error) {
	if r.actor == nil || r.actor.ID != actorID || r.actor.OwnerAccountID != accountID {
		return nil, nil
	}
	deletedAt = deletedAt.UTC()
	r.actor.IsSuspended = true
	r.actor.DeletedAt = &deletedAt
	return r.actor, nil
}

func (r *cachedActorBacking) ListOwnedAccountIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (r *cachedActorBacking) FindByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	actor, err := r.FindAnyByURI(ctx, uri)
	if actor != nil && actor.IsSuspended {
		return nil, err
	}
	return actor, err
}

func (r *cachedActorBacking) FindAnyByURI(_ context.Context, uri string) (*actors.Actor, error) {
	r.uriReads++
	if r.actor != nil && r.actor.URI == uri {
		copy := *r.actor
		return &copy, nil
	}
	return nil, nil
}

func (r *cachedActorBacking) FilterActiveRemoteIDs(context.Context, []string) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

func (r *cachedActorBacking) FindByPublicKeyID(_ context.Context, keyID string) (*actors.Actor, error) {
	r.keyReads++
	if r.actor != nil && r.actor.PublicKeyID == keyID && !r.actor.IsSuspended {
		copy := *r.actor
		return &copy, nil
	}
	return nil, nil
}

func (r *cachedActorBacking) UpsertRemoteActor(_ context.Context, actor actors.Actor) (*actors.Actor, error) {
	r.actor = &actor
	copy := actor
	return &copy, nil
}

func (r *cachedActorBacking) AddRemoteFeaturedNote(_ context.Context, actorURI, noteID string, limit int) (*actors.Actor, error) {
	if r.actor == nil || r.actor.URI != actorURI {
		return nil, nil
	}
	for _, existing := range r.actor.FeaturedNoteIDs {
		if existing == noteID {
			copy := *r.actor
			return &copy, nil
		}
	}
	if len(r.actor.FeaturedNoteIDs) < limit {
		r.actor.FeaturedNoteIDs = append(r.actor.FeaturedNoteIDs, noteID)
	}
	copy := *r.actor
	return &copy, nil
}

func (r *cachedActorBacking) RemoveRemoteFeaturedNote(_ context.Context, actorURI, noteID string) (*actors.Actor, error) {
	if r.actor == nil || r.actor.URI != actorURI {
		return nil, nil
	}
	featured := r.actor.FeaturedNoteIDs[:0]
	for _, existing := range r.actor.FeaturedNoteIDs {
		if existing != noteID {
			featured = append(featured, existing)
		}
	}
	r.actor.FeaturedNoteIDs = featured
	copy := *r.actor
	return &copy, nil
}

func (r *cachedActorBacking) MarkRemoteActorDeleted(_ context.Context, uri string) error {
	if r.actor != nil && r.actor.URI == uri {
		r.actor.IsSuspended = true
	}
	return nil
}

func TestCachedActorRepositoryCachesAndInvalidatesRemoteActor(t *testing.T) {
	host := "remote.example"
	backing := &cachedActorBacking{actor: &actors.Actor{
		ID: "actor-id", URI: "https://remote.example/users/alice", Host: &host,
		PublicKeyID: "https://remote.example/users/alice#main-key", PublicKeyPEM: "old-key",
	}}
	store := newMemoryValueStore()
	repository := NewCachedActorRepository(backing, store)

	for range 2 {
		actor, err := repository.FindAnyByURI(context.Background(), backing.actor.URI)
		if err != nil || actor == nil {
			t.Fatalf("FindAnyByURI actor=%+v err=%v", actor, err)
		}
	}
	if backing.uriReads != 1 {
		t.Fatalf("URI backing reads = %d, want 1", backing.uriReads)
	}
	if _, err := repository.FindByPublicKeyID(context.Background(), backing.actor.PublicKeyID); err != nil {
		t.Fatal(err)
	}
	if backing.keyReads != 0 {
		t.Fatalf("public key cache was not populated from URI lookup: reads=%d", backing.keyReads)
	}

	updated := *backing.actor
	oldKeyID := updated.PublicKeyID
	updated.PublicKeyID = updated.URI + "#rotated-key"
	updated.PublicKeyPEM = "new-key"
	if _, err := repository.UpsertRemoteActor(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	if _, found := loadJSON[actors.Actor](context.Background(), store, cacheKey("public-key", oldKeyID)); found {
		t.Fatal("old public key cache survived actor update")
	}
	actor, err := repository.FindByPublicKeyID(context.Background(), updated.PublicKeyID)
	if err != nil || actor == nil || actor.PublicKeyPEM != "new-key" {
		t.Fatalf("rotated key actor=%+v err=%v", actor, err)
	}
	if err := repository.MarkRemoteActorDeleted(context.Background(), updated.URI); err != nil {
		t.Fatal(err)
	}
	if actor, err := repository.FindByURI(context.Background(), updated.URI); err != nil || actor != nil {
		t.Fatalf("suspended actor remained active: actor=%+v err=%v", actor, err)
	}
}

type cachedInstanceBacking struct {
	instance *instances.Instance
	reads    int
}

func (r *cachedInstanceBacking) FindByHost(context.Context, string) (*instances.Instance, error) {
	r.reads++
	if r.instance == nil {
		return nil, nil
	}
	copy := *r.instance
	return &copy, nil
}

func (r *cachedInstanceBacking) Register(_ context.Context, host string, now time.Time) (*instances.Instance, bool, error) {
	if r.instance == nil {
		r.instance = &instances.Instance{Host: host, FirstRetrievedAt: now, SuspensionState: instances.SuspensionNone}
		return r.instance, true, nil
	}
	return r.instance, false, nil
}

func (r *cachedInstanceBacking) RecordReceived(context.Context, string, time.Time) (*instances.Instance, error) {
	return r.instance, nil
}

func (r *cachedInstanceBacking) RecordDeliverySuccess(_ context.Context, _ string, _ time.Time, status int) (*instances.Instance, error) {
	r.instance.LatestStatus = status
	r.instance.IsNotResponding = false
	return r.instance, nil
}

func (r *cachedInstanceBacking) RecordDeliveryFailure(_ context.Context, _ string, _ time.Time, status int) (*instances.Instance, error) {
	r.instance.LatestStatus = status
	r.instance.IsNotResponding = true
	return r.instance, nil
}

func (r *cachedInstanceBacking) UpdateMetadata(context.Context, string, instances.Metadata, time.Time) (*instances.Instance, error) {
	return r.instance, nil
}

func (r *cachedInstanceBacking) RefreshRelationshipCounts(context.Context, string, time.Time) (*instances.Instance, error) {
	return r.instance, nil
}

func (r *cachedInstanceBacking) SuspendGone(context.Context, string, time.Time) (*instances.Instance, error) {
	r.instance.SuspensionState = instances.SuspensionGone
	return r.instance, nil
}

func TestCachedInstanceRepositoryCachesAndRefreshesHealth(t *testing.T) {
	backing := &cachedInstanceBacking{instance: &instances.Instance{Host: "remote.example", SuspensionState: instances.SuspensionNone}}
	repository := NewCachedInstanceRepository(backing, newMemoryValueStore())
	for range 2 {
		if _, err := repository.FindByHost(context.Background(), "remote.example"); err != nil {
			t.Fatal(err)
		}
	}
	if backing.reads != 1 {
		t.Fatalf("instance backing reads = %d, want 1", backing.reads)
	}
	if _, err := repository.RecordDeliveryFailure(context.Background(), "remote.example", time.Now(), 503); err != nil {
		t.Fatal(err)
	}
	instance, err := repository.FindByHost(context.Background(), "remote.example")
	if err != nil || instance == nil || !instance.IsNotResponding || instance.LatestStatus != 503 || backing.reads != 1 {
		t.Fatalf("cached failure state instance=%+v reads=%d err=%v", instance, backing.reads, err)
	}
}

type countingWebFinger struct {
	calls int
}

func (r *countingWebFinger) ResolveActor(context.Context, string) (string, error) {
	r.calls++
	return "https://remote.example/users/alice", nil
}

func TestCachedWebFinger(t *testing.T) {
	upstream := &countingWebFinger{}
	resolver := NewCachedWebFinger(upstream, newMemoryValueStore())
	for range 2 {
		uri, err := resolver.ResolveActor(context.Background(), "alice@remote.example")
		if err != nil || uri != "https://remote.example/users/alice" {
			t.Fatalf("ResolveActor uri=%q err=%v", uri, err)
		}
	}
	if upstream.calls != 1 {
		t.Fatalf("WebFinger calls = %d, want 1", upstream.calls)
	}
}

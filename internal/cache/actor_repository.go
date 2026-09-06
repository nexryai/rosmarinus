package cache

import (
	"context"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

const (
	actorURICacheNamespace  = "actor-uri-v2"
	publicKeyCacheNamespace = "public-key-v2"
	actorURICacheTTL        = 5 * time.Minute
	publicKeyCacheTTL       = 12 * time.Hour
)

type CachedActorRepository struct {
	repository actorRepository
	store      ValueStore
}

type actorRepository interface {
	actors.Repository
	FindByID(context.Context, string) (*actors.Actor, error)
}

func NewCachedActorRepository(repository actorRepository, store ValueStore) *CachedActorRepository {
	return &CachedActorRepository{repository: repository, store: store}
}

func (r *CachedActorRepository) FindByID(ctx context.Context, id string) (*actors.Actor, error) {
	return r.repository.FindByID(ctx, id)
}

func (r *CachedActorRepository) FindAnyByID(ctx context.Context, id string) (*actors.Actor, error) {
	return r.repository.FindAnyByID(ctx, id)
}

func (r *CachedActorRepository) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	return r.repository.FindLocalByID(ctx, id)
}

func (r *CachedActorRepository) FindLocalByUsername(ctx context.Context, username string) (*actors.Actor, error) {
	return r.repository.FindLocalByUsername(ctx, username)
}

func (r *CachedActorRepository) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	return r.repository.FindOwnedLocalByID(ctx, accountID, actorID)
}

func (r *CachedActorRepository) FindOwnedLocalByIDIncludingDeleted(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	return r.repository.FindOwnedLocalByIDIncludingDeleted(ctx, accountID, actorID)
}

func (r *CachedActorRepository) ListOwnedLocalActorsPage(ctx context.Context, accountID, afterID string, limit int, includeDeleted bool) ([]actors.Actor, error) {
	return r.repository.ListOwnedLocalActorsPage(ctx, accountID, afterID, limit, includeDeleted)
}

func (r *CachedActorRepository) FindLocalForDeliveryByID(ctx context.Context, actorID string) (*actors.Actor, error) {
	return r.repository.FindLocalForDeliveryByID(ctx, actorID)
}

func (r *CachedActorRepository) CreateOwnedLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	return r.repository.CreateOwnedLocalActor(ctx, actor)
}

func (r *CachedActorRepository) UpdateOwnedLocalActor(ctx context.Context, accountID, actorID string, patch actors.ActorPatch) (*actors.Actor, error) {
	return r.repository.UpdateOwnedLocalActor(ctx, accountID, actorID, patch)
}

func (r *CachedActorRepository) MarkOwnedLocalActorDeleted(ctx context.Context, accountID, actorID string, deletedAt time.Time) (*actors.Actor, error) {
	return r.repository.MarkOwnedLocalActorDeleted(ctx, accountID, actorID, deletedAt)
}

func (r *CachedActorRepository) SetOwnedLocalActorSuspended(ctx context.Context, accountID, actorID string, suspended bool, changedAt time.Time) (*actors.Actor, error) {
	return r.repository.SetOwnedLocalActorSuspended(ctx, accountID, actorID, suspended, changedAt)
}

func (r *CachedActorRepository) ListOwnedAccountIDs(ctx context.Context) ([]string, error) {
	return r.repository.ListOwnedAccountIDs(ctx)
}

func (r *CachedActorRepository) FindByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	actor, err := r.FindAnyByURI(ctx, uri)
	if actor != nil && actor.IsSuspended {
		return nil, err
	}
	return actor, err
}

func (r *CachedActorRepository) FindAnyByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	key := cacheKey(actorURICacheNamespace, uri)
	if actor, found := loadJSON[actors.Actor](ctx, r.store, key); found {
		return actor, nil
	}
	actor, err := r.repository.FindAnyByURI(ctx, uri)
	if err != nil || actor == nil {
		return actor, err
	}
	r.cacheActor(ctx, actor)
	return actor, nil
}

func (r *CachedActorRepository) FilterActiveRemoteIDs(ctx context.Context, ids []string) (map[string]struct{}, error) {
	return r.repository.FilterActiveRemoteIDs(ctx, ids)
}

func (r *CachedActorRepository) FindByPublicKeyID(ctx context.Context, keyID string) (*actors.Actor, error) {
	key := cacheKey(publicKeyCacheNamespace, keyID)
	if actor, found := loadJSON[actors.Actor](ctx, r.store, key); found {
		if actor.IsSuspended {
			return nil, nil
		}
		return actor, nil
	}
	actor, err := r.repository.FindByPublicKeyID(ctx, keyID)
	if err != nil || actor == nil {
		return actor, err
	}
	r.cacheActor(ctx, actor)
	return actor, nil
}

func (r *CachedActorRepository) UpsertRemoteActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	existing, err := r.repository.FindAnyByURI(ctx, actor.URI)
	if err != nil {
		return nil, err
	}
	updated, err := r.repository.UpsertRemoteActor(ctx, actor)
	if err != nil {
		return nil, err
	}
	r.invalidateActor(ctx, existing)
	r.cacheActor(ctx, updated)
	return updated, nil
}

func (r *CachedActorRepository) AddRemoteFeaturedNote(ctx context.Context, actorURI, noteID string, limit int) (*actors.Actor, error) {
	existing, err := r.repository.FindAnyByURI(ctx, actorURI)
	if err != nil {
		return nil, err
	}
	updated, err := r.repository.AddRemoteFeaturedNote(ctx, actorURI, noteID, limit)
	if err != nil {
		return nil, err
	}
	r.invalidateActor(ctx, existing)
	r.cacheActor(ctx, updated)
	return updated, nil
}

func (r *CachedActorRepository) RemoveRemoteFeaturedNote(ctx context.Context, actorURI, noteID string) (*actors.Actor, error) {
	existing, err := r.repository.FindAnyByURI(ctx, actorURI)
	if err != nil {
		return nil, err
	}
	updated, err := r.repository.RemoveRemoteFeaturedNote(ctx, actorURI, noteID)
	if err != nil {
		return nil, err
	}
	r.invalidateActor(ctx, existing)
	r.cacheActor(ctx, updated)
	return updated, nil
}

func (r *CachedActorRepository) MarkRemoteActorDeleted(ctx context.Context, uri string) error {
	existing, err := r.repository.FindAnyByURI(ctx, uri)
	if err != nil {
		return err
	}
	if err := r.repository.MarkRemoteActorDeleted(ctx, uri); err != nil {
		return err
	}
	r.invalidateActor(ctx, existing)
	_ = r.store.Delete(ctx, cacheKey(actorURICacheNamespace, uri))
	return nil
}

func (r *CachedActorRepository) cacheActor(ctx context.Context, actor *actors.Actor) {
	if actor == nil || actor.Host == nil {
		return
	}
	storeJSON(ctx, r.store, cacheKey(actorURICacheNamespace, actor.URI), actor, actorURICacheTTL)
	if actor.PublicKeyID != "" {
		storeJSON(ctx, r.store, cacheKey(publicKeyCacheNamespace, actor.PublicKeyID), actor, publicKeyCacheTTL)
	}
}

func (r *CachedActorRepository) invalidateActor(ctx context.Context, actor *actors.Actor) {
	if actor == nil {
		return
	}
	_ = r.store.Delete(ctx,
		cacheKey(actorURICacheNamespace, actor.URI),
		cacheKey(publicKeyCacheNamespace, actor.PublicKeyID),
	)
}

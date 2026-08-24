package cache

import (
	"context"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

const (
	actorURICacheTTL  = 5 * time.Minute
	publicKeyCacheTTL = 12 * time.Hour
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

func (r *CachedActorRepository) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	return r.repository.FindLocalByID(ctx, id)
}

func (r *CachedActorRepository) FindLocalByUsername(ctx context.Context, username string) (*actors.Actor, error) {
	return r.repository.FindLocalByUsername(ctx, username)
}

func (r *CachedActorRepository) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	return r.repository.FindOwnedLocalByID(ctx, accountID, actorID)
}

func (r *CachedActorRepository) CreateOwnedLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	return r.repository.CreateOwnedLocalActor(ctx, actor)
}

func (r *CachedActorRepository) SuspendOwnedLocalActors(ctx context.Context, accountID string) (int64, error) {
	return r.repository.SuspendOwnedLocalActors(ctx, accountID)
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
	key := cacheKey("actor-uri", uri)
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
	key := cacheKey("public-key", keyID)
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

func (r *CachedActorRepository) MarkRemoteActorDeleted(ctx context.Context, uri string) error {
	existing, err := r.repository.FindAnyByURI(ctx, uri)
	if err != nil {
		return err
	}
	if err := r.repository.MarkRemoteActorDeleted(ctx, uri); err != nil {
		return err
	}
	r.invalidateActor(ctx, existing)
	_ = r.store.Delete(ctx, cacheKey("actor-uri", uri))
	return nil
}

func (r *CachedActorRepository) cacheActor(ctx context.Context, actor *actors.Actor) {
	if actor == nil || actor.Host == nil {
		return
	}
	storeJSON(ctx, r.store, cacheKey("actor-uri", actor.URI), actor, actorURICacheTTL)
	if actor.PublicKeyID != "" {
		storeJSON(ctx, r.store, cacheKey("public-key", actor.PublicKeyID), actor, publicKeyCacheTTL)
	}
}

func (r *CachedActorRepository) invalidateActor(ctx context.Context, actor *actors.Actor) {
	if actor == nil {
		return
	}
	_ = r.store.Delete(ctx,
		cacheKey("actor-uri", actor.URI),
		cacheKey("public-key", actor.PublicKeyID),
	)
}

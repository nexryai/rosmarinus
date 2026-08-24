package cache

import (
	"context"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/instances"
)

const instanceCacheTTL = time.Hour

type CachedInstanceRepository struct {
	repository instances.Repository
	store      ValueStore
}

func NewCachedInstanceRepository(repository instances.Repository, store ValueStore) *CachedInstanceRepository {
	return &CachedInstanceRepository{repository: repository, store: store}
}

func (r *CachedInstanceRepository) FindByHost(ctx context.Context, host string) (*instances.Instance, error) {
	key := cacheKey("instance", host)
	if instance, found := loadJSON[instances.Instance](ctx, r.store, key); found {
		return instance, nil
	}
	instance, err := r.repository.FindByHost(ctx, host)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) Register(ctx context.Context, host string, now time.Time) (*instances.Instance, bool, error) {
	if instance, err := r.FindByHost(ctx, host); err != nil || instance != nil {
		return instance, false, err
	}
	instance, created, err := r.repository.Register(ctx, host, now)
	r.cache(ctx, instance)
	return instance, created, err
}

func (r *CachedInstanceRepository) RecordReceived(ctx context.Context, host string, now time.Time) (*instances.Instance, error) {
	instance, err := r.repository.RecordReceived(ctx, host, now)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) RecordDeliverySuccess(ctx context.Context, host string, now time.Time, status int) (*instances.Instance, error) {
	instance, err := r.repository.RecordDeliverySuccess(ctx, host, now, status)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) RecordDeliveryFailure(ctx context.Context, host string, now time.Time, status int) (*instances.Instance, error) {
	instance, err := r.repository.RecordDeliveryFailure(ctx, host, now, status)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) UpdateMetadata(ctx context.Context, host string, metadata instances.Metadata, now time.Time) (*instances.Instance, error) {
	instance, err := r.repository.UpdateMetadata(ctx, host, metadata, now)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) RefreshRelationshipCounts(ctx context.Context, host string, now time.Time) (*instances.Instance, error) {
	instance, err := r.repository.RefreshRelationshipCounts(ctx, host, now)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) SuspendGone(ctx context.Context, host string, now time.Time) (*instances.Instance, error) {
	instance, err := r.repository.SuspendGone(ctx, host, now)
	r.cache(ctx, instance)
	return instance, err
}

func (r *CachedInstanceRepository) cache(ctx context.Context, instance *instances.Instance) {
	if instance != nil {
		storeJSON(ctx, r.store, cacheKey("instance", instance.Host), instance, instanceCacheTTL)
	}
}

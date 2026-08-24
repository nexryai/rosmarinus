package cache

import (
	"context"
	"strings"
	"time"
)

const webFingerCacheTTL = 5 * time.Minute

type WebFingerResolver interface {
	ResolveActor(context.Context, string) (string, error)
}

type CachedWebFinger struct {
	resolver WebFingerResolver
	store    ValueStore
}

func NewCachedWebFinger(resolver WebFingerResolver, store ValueStore) *CachedWebFinger {
	return &CachedWebFinger{resolver: resolver, store: store}
}

func (r *CachedWebFinger) ResolveActor(ctx context.Context, query string) (string, error) {
	key := cacheKey("webfinger", strings.TrimSpace(query))
	if value, found := loadJSON[string](ctx, r.store, key); found {
		return *value, nil
	}
	uri, err := r.resolver.ResolveActor(ctx, query)
	if err != nil {
		return "", err
	}
	storeJSON(ctx, r.store, key, uri, webFingerCacheTTL)
	return uri, nil
}

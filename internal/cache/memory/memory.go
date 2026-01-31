package memory

import (
	"context"
	"time"

	"github.com/patrickmn/go-cache"
)

type InMemoryCache struct {
	c *cache.Cache
}

func New() *InMemoryCache {
	return &InMemoryCache{
		c: cache.New(cache.NoExpiration, 10*time.Minute),
	}
}

func (m *InMemoryCache) Get(ctx context.Context, key string) (string, error) {
	val, found := m.c.Get(key)
	if !found {
		return "", nil
	}
	return val.(string), nil
}

func (m *InMemoryCache) Set(ctx context.Context, key string, value string, ttlSeconds int) error {
	m.c.Set(key, value, time.Duration(ttlSeconds)*time.Second)
	return nil
}

func (m *InMemoryCache) Delete(ctx context.Context, key string) error {
	m.c.Delete(key)
	return nil
}

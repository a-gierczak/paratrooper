package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
}

func New(connString string) (*RedisCache, error) {
	opt, err := redis.ParseURL(connString)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)
	return &RedisCache{
		client: rdb,
	}, nil
}

func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (r *RedisCache) Set(ctx context.Context, key string, value string, ttlSeconds int) error {
	return r.client.Set(ctx, key, value, time.Duration(ttlSeconds)*time.Second).Err()
}

func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

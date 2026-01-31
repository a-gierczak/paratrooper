package cache

import (
	"context"

	memorycache "github.com/a-gierczak/paratrooper/internal/cache/memory"
	rediscache "github.com/a-gierczak/paratrooper/internal/cache/redis"
	"github.com/a-gierczak/paratrooper/internal/logger"
)

type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttlSeconds int) error
	Delete(ctx context.Context, key string) error
}

type Config struct {
	Driver   string `env:"CACHE_DRIVER"    validate:"required,oneof=memory redis,default=memory"`
	RedisURL string `env:"CACHE_REDIS_URL"`
}

func New(ctx context.Context, config Config) (Cache, error) {
	log := logger.FromContext(ctx)
	if config.Driver == "redis" {
		log.Info("initializing redis cache")
		return rediscache.New(config.RedisURL)
	}

	log.Info("initializing in-memory cache")
	return memorycache.New(), nil
}

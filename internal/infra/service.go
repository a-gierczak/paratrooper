package infra

import (
	"asset-server/internal/cache"
	"asset-server/internal/queue"
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service interface {
	HealthCheck(ctx context.Context) error
	Cache() cache.Cache
}

type service struct {
	pgPool    *pgxpool.Pool
	queueConn *queue.Connection
	cache     cache.Cache
}

func NewService(pgPool *pgxpool.Pool, queueConn *queue.Connection, cache cache.Cache) Service {
	return &service{pgPool, queueConn, cache}
}

func (svc *service) HealthCheck(ctx context.Context) error {
	if err := svc.pgPool.Ping(ctx); err != nil {
		return err
	}

	if err := svc.queueConn.HealthCheck(); err != nil {
		return err
	}

	return nil
}

func (svc *service) Cache() cache.Cache {
	return svc.cache
}

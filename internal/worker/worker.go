package worker

import (
	"asset-server/generated/db"
	"asset-server/internal/logger"
	"asset-server/internal/queue"
	"asset-server/internal/storage"
	"asset-server/internal/update"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Config struct {
	DebugMode   bool   `env:"DEBUG"`
	PostgresDSN string `env:"POSTGRES_DSN"`
	NATSURL     string `env:"NATS_URL"`
	Storage     storage.Config
}

func Run(config Config, log *zap.Logger) error {
	ctx := logger.ContextWithLogger(context.Background(), log)

	// connect to postgres
	pgConn, err := pgxpool.New(ctx, config.PostgresDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}
	queries := db.New(pgConn)

	// connect to nats
	queueConn, err := queue.Connect(ctx, config.NATSURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// init storage
	storageDriver, err := storage.Init(ctx, &config.Storage)
	if err != nil {
		return fmt.Errorf("failed to init storage: %w", err)
	}
	updateSvc := update.NewService(queries, pgConn, storageDriver, queueConn)
	updateProcessor := update.NewProcessor(updateSvc, storageDriver, queueConn)

	return updateProcessor.StartWorker(ctx)
}

package api

import (
	"asset-server/generated/api"
	"asset-server/generated/db"
	"asset-server/internal/cache"
	"asset-server/internal/codepush"
	"asset-server/internal/expo"
	"asset-server/internal/infra"
	"asset-server/internal/logger"
	"asset-server/internal/project"
	"asset-server/internal/queue"
	"asset-server/internal/storage"
	"asset-server/internal/update"
	"context"
	"fmt"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Config struct {
	PostgresDSN string `env:"POSTGRES_DSN"`
	DebugMode   bool   `env:"DEBUG"`
	NATSURL     string `env:"NATS_URL"`
	Storage     storage.Config
	Cache       cache.Config
}

func Run(config Config, log *zap.Logger) error {
	var err error

	if config.DebugMode {
		gin.SetMode(gin.DebugMode)
		gin.DebugPrintRouteFunc = func(httpMethod, absolutePath, handlerName string, nuHandlers int) {
			log.Debug(
				"GIN - registered route",
				zap.String("httpMethod", httpMethod),
				zap.String("absolutePath", absolutePath),
				zap.String("handlerName", handlerName),
				zap.Int("nuHandlers", nuHandlers),
			)
		}
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	gin.DefaultWriter = zap.NewStdLog(log).Writer()

	ctx := logger.ContextWithLogger(context.Background(), log)

	// connect to postgres
	pgConn, err := pgxpool.New(ctx, config.PostgresDSN)
	if err != nil {
		return fmt.Errorf("failed create a connection pool to postgres: %w", err)
	}
	queries := db.New(pgConn)

	// connect to nats
	queueConn, err := queue.Connect(ctx, config.NATSURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer queueConn.Close()

	// init storage
	storageDriver, err := storage.Init(ctx, &config.Storage)
	if err != nil {
		return fmt.Errorf("failed to init storage: %w", err)
	}

	r := gin.New()
	r.Use(logger.NewMiddleware(log))
	r.Use(ginzap.Ginzap(log, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(log, true))
	r.Use(NewErrorHandlingMiddleware())

	// init cache
	cacheDriver, err := cache.New(ctx, config.Cache)
	if err != nil {
		return fmt.Errorf("failed to init cache: %w", err)
	}

	updateSvc := update.NewService(queries, pgConn, storageDriver, queueConn)
	server := NewServer(
		updateSvc,
		codepush.NewService(queries, storageDriver),
		expo.NewService(queries, storageDriver),
		project.NewService(queries),
		infra.NewService(pgConn, queueConn, cacheDriver),
	)

	h := api.NewStrictHandler(server, []api.StrictMiddlewareFunc{
		logger.NewOperationNameStrictMiddleware(),
		validateRequestMiddleware,
	})
	if storageDriver.Provider() == storage.ProviderLocal {
		addStorageRoutes(r, storageDriver)
	}
	api.RegisterHandlers(r, h)

	log.Info("API server started")
	return r.Run()
}

// validateRequestMiddleware validates the request parameters using the validator library.
// This needs to be done manually because the generated code doesn't validate the query & path parameters.
func validateRequestMiddleware(
	handler api.StrictHandlerFunc,
	operationID string,
) api.StrictHandlerFunc {
	return func(ctx *gin.Context, request interface{}) (response interface{}, err error) {
		if err := binding.Validator.ValidateStruct(request); err != nil {
			ctx.Abort()
			return nil, err
		}
		return handler(ctx, request)
	}
}

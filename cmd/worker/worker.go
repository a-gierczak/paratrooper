package main

import (
	"asset-server/internal/logger"
	"asset-server/internal/worker"
	"github.com/Netflix/go-env"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"log"
)

func main() {
	_ = godotenv.Load()

	var config worker.Config
	_, err := env.UnmarshalFromEnviron(&config)
	if err != nil {
		log.Fatal(err)
	}

	logger, err := logger.NewLogger(config.DebugMode)
	if err != nil {
		log.Fatal(err)
	}

	defer logger.Sync()

	if err := worker.Run(config, logger); err != nil {
		logger.Fatal("failed to run worker", zap.Error(err))
	}
}

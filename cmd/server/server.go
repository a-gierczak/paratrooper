package main

import (
	"log"

	"github.com/Netflix/go-env"
	"github.com/a-gierczak/paratrooper/internal/api"
	"github.com/a-gierczak/paratrooper/internal/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	_ = godotenv.Load()

	var config api.Config
	_, err := env.UnmarshalFromEnviron(&config)
	if err != nil {
		log.Fatal(err)
	}

	logger, err := logger.NewLogger(config.DebugMode)
	if err != nil {
		log.Fatal(err)
	}

	defer logger.Sync()

	if err := api.Run(config, logger); err != nil {
		logger.Fatal("failed to run api", zap.Error(err))
	}
}

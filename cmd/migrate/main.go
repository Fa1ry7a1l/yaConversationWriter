package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"yaConversationWriter/internal/app"
	"yaConversationWriter/internal/config"
	"yaConversationWriter/internal/infrastructure/repository/postgres"
)

const defaultConfigPath = "configs/config.example.yaml"

func main() {
	os.Exit(run())
}

func run() int {
	direction := flag.String("direction", "up", "migration direction: up or down")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	configPath := os.Getenv("APP_CONFIG_PATH")
	if configPath == "" {
		configPath = defaultConfigPath
	}
	cfg, err := config.NewYAMLSource(configPath, os.LookupEnv).Load(ctx)
	if err != nil {
		slog.Error("load configuration", "error", err)
		return 1
	}
	logger := app.NewLogger(cfg.Logging)
	if cfg.Storage.Provider != config.ProviderPostgres {
		logger.Error("migration requires storage.provider=postgres")
		return 1
	}

	repository, err := postgres.New(cfg.Storage.Postgres, logger)
	if err != nil {
		logger.Error("build PostgreSQL repository", "error", err)
		return 1
	}
	if err := repository.Start(ctx); err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		return 1
	}
	defer func() { _ = repository.Shutdown(context.Background()) }()

	switch *direction {
	case "up":
		err = repository.MigrateUp(ctx)
	case "down":
		err = repository.MigrateDown(ctx)
	default:
		logger.Error("unsupported migration direction", "direction", *direction)
		return 1
	}
	if err != nil {
		logger.Error("migration failed", "direction", *direction, "error", err)
		return 1
	}
	logger.Info("migration completed", "direction", *direction)
	return 0
}

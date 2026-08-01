package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"yaConversationWriter/internal/app"
	"yaConversationWriter/internal/config"
)

const defaultConfigPath = "configs/config.example.yaml"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	application, err := app.Build(cfg, logger)
	if err != nil {
		logger.Error("build application", "error", err)
		return 1
	}

	if err := application.Run(ctx); err != nil {
		logger.Error("application stopped with error", "error", err)
		return 1
	}

	return 0
}

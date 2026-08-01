package app

import (
	"fmt"
	"log/slog"

	"yaConversationWriter/internal/config"
	llmfactory "yaConversationWriter/internal/infrastructure/llm"
	repositoryfactory "yaConversationWriter/internal/infrastructure/repository"
	speechfactory "yaConversationWriter/internal/infrastructure/speech"
)

func Build(cfg config.Config, logger *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	repository, err := repositoryfactory.New(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("build repository: %w", err)
	}
	speechClient, err := speechfactory.New(cfg.Speech)
	if err != nil {
		return nil, fmt.Errorf("build speech client: %w", err)
	}
	llmClient, err := llmfactory.New(cfg.LLM)
	if err != nil {
		return nil, fmt.Errorf("build LLM client: %w", err)
	}

	for _, listener := range cfg.Listeners {
		if listener.Enabled {
			return nil, fmt.Errorf("listener %q: Telegram adapter is not available in this increment", listener.Name)
		}
	}

	return New(logger, cfg.App.ShutdownTimeout, Dependencies{
		Repository: repository,
		Speech:     speechClient,
		LLM:        llmClient,
	})
}

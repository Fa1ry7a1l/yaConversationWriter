package app

import (
	"fmt"
	"log/slog"

	"yaConversationWriter/internal/config"
	llmfactory "yaConversationWriter/internal/infrastructure/llm"
	repositoryfactory "yaConversationWriter/internal/infrastructure/repository"
	speechfactory "yaConversationWriter/internal/infrastructure/speech"
	"yaConversationWriter/internal/service"
	"yaConversationWriter/internal/transport/telegram"
	"yaConversationWriter/internal/worker"
)

func Build(cfg config.Config, logger *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	repository, storageLifecycle, err := repositoryfactory.New(cfg.Storage, logger)
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
	processor, err := service.NewProcessor(repository, speechClient, llmClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build meeting processor: %w", err)
	}
	workers, err := worker.New(worker.Config{
		Count:       cfg.Workers.Count,
		QueueSize:   cfg.Workers.QueueSize,
		TaskTimeout: cfg.Workers.TaskTimeout,
	}, processor, logger)
	if err != nil {
		return nil, fmt.Errorf("build worker pool: %w", err)
	}
	meetingApplication, err := service.NewMeetings(repository, llmClient, workers, logger)
	if err != nil {
		return nil, fmt.Errorf("build meeting application: %w", err)
	}

	listeners := make([]Listener, 0, len(cfg.Listeners))
	for _, listenerConfig := range cfg.Listeners {
		if !listenerConfig.Enabled {
			continue
		}
		listener, err := telegram.New(listenerConfig.Name, listenerConfig.Token, meetingApplication, logger)
		if err != nil {
			return nil, fmt.Errorf("build listener %q: %w", listenerConfig.Name, err)
		}
		listeners = append(listeners, listener)
	}

	return New(
		WithLogger(logger),
		WithShutdownTimeout(cfg.App.ShutdownTimeout),
		WithDependencies(Dependencies{
			Repository:  repository,
			Speech:      speechClient,
			LLM:         llmClient,
			Application: meetingApplication,
			Background:  []Lifecycle{storageLifecycle, workers},
		}),
		WithListeners(listeners...),
	)
}

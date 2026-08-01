package repository

import (
	"context"
	"fmt"
	"log/slog"

	"yaConversationWriter/internal/config"
	"yaConversationWriter/internal/infrastructure/repository/memory"
	"yaConversationWriter/internal/infrastructure/repository/postgres"
	"yaConversationWriter/internal/ports"
)

type Lifecycle interface {
	Name() string
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

func New(cfg config.Storage, logger *slog.Logger) (ports.Repository, Lifecycle, error) {
	switch cfg.Provider {
	case config.ProviderMemory:
		return memory.New(), nopLifecycle{}, nil
	case config.ProviderPostgres:
		repository, err := postgres.New(cfg.Postgres, logger)
		if err != nil {
			return nil, nil, err
		}
		return repository, repository, nil
	default:
		return nil, nil, fmt.Errorf("storage provider %q is not supported", cfg.Provider)
	}
}

type nopLifecycle struct{}

func (nopLifecycle) Name() string                   { return "memory-storage" }
func (nopLifecycle) Start(context.Context) error    { return nil }
func (nopLifecycle) Shutdown(context.Context) error { return nil }

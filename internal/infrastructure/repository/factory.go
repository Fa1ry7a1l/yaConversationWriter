package repository

import (
	"fmt"

	"yaConversationWriter/internal/config"
	"yaConversationWriter/internal/infrastructure/repository/memory"
	"yaConversationWriter/internal/ports"
)

func New(cfg config.Storage) (ports.Repository, error) {
	switch cfg.Provider {
	case config.ProviderMemory:
		return memory.New(), nil
	default:
		return nil, fmt.Errorf("storage provider %q is not supported", cfg.Provider)
	}
}

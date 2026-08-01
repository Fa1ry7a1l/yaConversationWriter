package speech

import (
	"errors"
	"fmt"

	"yaConversationWriter/internal/config"
	speechmock "yaConversationWriter/internal/infrastructure/speech/mock"
	"yaConversationWriter/internal/ports"
)

func New(cfg config.Speech) (ports.SpeechClient, error) {
	switch cfg.Provider {
	case config.ProviderMock:
		var configuredError error
		if cfg.Mock.Error != "" {
			configuredError = errors.New(cfg.Mock.Error)
		}
		return speechmock.New(speechmock.Config{
			Transcript: cfg.Mock.Transcript,
			Delay:      cfg.Mock.Delay,
			Err:        configuredError,
		}), nil
	default:
		return nil, fmt.Errorf("speech provider %q is not supported", cfg.Provider)
	}
}

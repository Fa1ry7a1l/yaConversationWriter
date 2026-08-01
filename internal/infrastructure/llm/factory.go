package llm

import (
	"errors"
	"fmt"

	"yaConversationWriter/internal/config"
	llmmock "yaConversationWriter/internal/infrastructure/llm/mock"
	"yaConversationWriter/internal/ports"
)

func New(cfg config.LLM) (ports.LLMClient, error) {
	switch cfg.Provider {
	case config.ProviderMock:
		var summaryError error
		if cfg.Mock.SummaryError != "" {
			summaryError = errors.New(cfg.Mock.SummaryError)
		}
		var answerError error
		if cfg.Mock.AnswerError != "" {
			answerError = errors.New(cfg.Mock.AnswerError)
		}
		return llmmock.New(llmmock.Config{
			Summary:      cfg.Mock.Summary,
			Answer:       cfg.Mock.Answer,
			SummaryDelay: cfg.Mock.SummaryDelay,
			AnswerDelay:  cfg.Mock.AnswerDelay,
			SummaryErr:   summaryError,
			AnswerErr:    answerError,
		}), nil
	default:
		return nil, fmt.Errorf("LLM provider %q is not supported", cfg.Provider)
	}
}

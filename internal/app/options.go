package app

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Option configures a value of T. The generic contract keeps option
// application reusable while concrete With functions expose a focused API.
type Option[T any] interface {
	apply(*T) error
}

type optionFunc[T any] func(*T) error

func (option optionFunc[T]) apply(target *T) error {
	return option(target)
}

func applyOptions[T any](target *T, options ...Option[T]) error {
	for index, option := range options {
		if option == nil {
			return fmt.Errorf("option %d is nil", index)
		}
		if err := option.apply(target); err != nil {
			return fmt.Errorf("apply option %d: %w", index, err)
		}
	}
	return nil
}

func WithLogger(logger *slog.Logger) Option[App] {
	return optionFunc[App](func(application *App) error {
		if logger == nil {
			return errors.New("logger is required")
		}
		application.logger = logger
		return nil
	})
}

func WithShutdownTimeout(timeout time.Duration) Option[App] {
	return optionFunc[App](func(application *App) error {
		if timeout <= 0 {
			return errors.New("shutdown timeout must be greater than zero")
		}
		application.shutdownTimeout = timeout
		return nil
	})
}

func WithDependencies(dependencies Dependencies) Option[App] {
	dependencies.Background = append([]Lifecycle(nil), dependencies.Background...)
	return optionFunc[App](func(application *App) error {
		if dependencies.Repository == nil || dependencies.Speech == nil || dependencies.LLM == nil || dependencies.Application == nil {
			return errors.New("repository, speech client, LLM client and application service are required")
		}
		for index, component := range dependencies.Background {
			if component == nil {
				return fmt.Errorf("background component %d is nil", index)
			}
		}

		application.background = dependencies.Background
		application.dependencies = dependencies
		return nil
	})
}

func WithListeners(listeners ...Listener) Option[App] {
	listeners = append([]Listener(nil), listeners...)
	return optionFunc[App](func(application *App) error {
		for index, listener := range listeners {
			if listener == nil {
				return fmt.Errorf("listener %d is nil", index)
			}
		}
		application.listeners = append(application.listeners, listeners...)
		return nil
	})
}

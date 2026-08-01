package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"yaConversationWriter/internal/ports"
)

// Lifecycle is implemented by long-running application components.
type Lifecycle interface {
	Name() string
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Listener is the transport-facing lifecycle contract. The alias keeps the
// application lifecycle uniform without coupling workers to a transport type.
type Listener = Lifecycle

type Dependencies struct {
	Repository  ports.Repository
	Speech      ports.SpeechClient
	LLM         ports.LLMClient
	Application ports.MeetingApplication
	Background  []Lifecycle
}

type App struct {
	logger          *slog.Logger
	shutdownTimeout time.Duration
	background      []Lifecycle
	listeners       []Listener
	dependencies    Dependencies
}

func New(logger *slog.Logger, shutdownTimeout time.Duration, dependencies Dependencies, listeners ...Listener) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if shutdownTimeout <= 0 {
		return nil, errors.New("shutdown timeout must be greater than zero")
	}
	if dependencies.Repository == nil || dependencies.Speech == nil || dependencies.LLM == nil || dependencies.Application == nil {
		return nil, errors.New("repository, speech client, LLM client and application service are required")
	}
	for i, component := range dependencies.Background {
		if component == nil {
			return nil, fmt.Errorf("background component %d is nil", i)
		}
	}
	for i, listener := range listeners {
		if listener == nil {
			return nil, fmt.Errorf("listener %d is nil", i)
		}
	}

	return &App{
		logger:          logger,
		shutdownTimeout: shutdownTimeout,
		background:      append([]Lifecycle(nil), dependencies.Background...),
		listeners:       append([]Listener(nil), listeners...),
		dependencies:    dependencies,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	components := make([]Lifecycle, 0, len(a.background)+len(a.listeners))
	components = append(components, a.background...)
	components = append(components, a.listeners...)
	started := make([]Lifecycle, 0, len(components))
	for _, component := range components {
		a.logger.Info("starting component", "component", component.Name())
		if err := component.Start(ctx); err != nil {
			startErr := fmt.Errorf("start component %q: %w", component.Name(), err)
			a.logger.Error("component start failed", "component", component.Name(), "error", err)
			shutdownErr := a.shutdown(started)
			return errors.Join(startErr, shutdownErr)
		}
		started = append(started, component)
		a.logger.Info("component started", "component", component.Name())
	}

	a.logger.Info("application started", "components", len(started), "listeners", len(a.listeners))
	<-ctx.Done()
	a.logger.Info("application shutdown requested")
	return a.shutdown(started)
}

func (a *App) shutdown(started []Lifecycle) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		component := started[i]
		a.logger.Info("stopping component", "component", component.Name())
		if err := component.Shutdown(shutdownCtx); err != nil {
			wrapped := fmt.Errorf("shutdown component %q: %w", component.Name(), err)
			errs = append(errs, wrapped)
			a.logger.Error("component shutdown failed", "component", component.Name(), "error", err)
			continue
		}
		a.logger.Info("component stopped", "component", component.Name())
	}
	return errors.Join(errs...)
}

func (a *App) Application() ports.MeetingApplication {
	return a.dependencies.Application
}

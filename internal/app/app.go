package app

import (
	"context"
	"errors"
	"fmt"
	"iter"
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

func New(options ...Option[App]) (*App, error) {
	application := &App{
		logger:          slog.Default(),
		shutdownTimeout: 10 * time.Second,
	}
	if err := applyOptions(application, options...); err != nil {
		return nil, err
	}
	if application.dependencies.Application == nil {
		return nil, errors.New("application dependencies are required")
	}
	return application, nil
}

func (a *App) Run(ctx context.Context) error {
	started := make([]Lifecycle, 0, len(a.background)+len(a.listeners))
	for component := range a.components() {
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

func (a *App) components() iter.Seq[Lifecycle] {
	return func(yield func(Lifecycle) bool) {
		for _, components := range [][]Lifecycle{a.background, a.listeners} {
			for _, component := range components {
				if !yield(component) {
					return
				}
			}
		}
	}
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

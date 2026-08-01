package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"yaConversationWriter/internal/ports"
)

// Listener is the lifecycle contract consumed by the application. Concrete
// transports, including the future Telegram adapter, implement this contract.
type Listener interface {
	Name() string
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type Dependencies struct {
	Repository ports.Repository
	Speech     ports.SpeechClient
	LLM        ports.LLMClient
}

type App struct {
	logger          *slog.Logger
	shutdownTimeout time.Duration
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
	if dependencies.Repository == nil || dependencies.Speech == nil || dependencies.LLM == nil {
		return nil, errors.New("repository, speech client and LLM client are required")
	}
	for i, listener := range listeners {
		if listener == nil {
			return nil, fmt.Errorf("listener %d is nil", i)
		}
	}

	return &App{
		logger:          logger,
		shutdownTimeout: shutdownTimeout,
		listeners:       append([]Listener(nil), listeners...),
		dependencies:    dependencies,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	started := make([]Listener, 0, len(a.listeners))
	for _, listener := range a.listeners {
		a.logger.Info("starting listener", "listener", listener.Name())
		if err := listener.Start(ctx); err != nil {
			startErr := fmt.Errorf("start listener %q: %w", listener.Name(), err)
			a.logger.Error("listener start failed", "listener", listener.Name(), "error", err)
			shutdownErr := a.shutdown(started)
			return errors.Join(startErr, shutdownErr)
		}
		started = append(started, listener)
		a.logger.Info("listener started", "listener", listener.Name())
	}

	a.logger.Info("application started", "listeners", len(started))
	<-ctx.Done()
	a.logger.Info("application shutdown requested")
	return a.shutdown(started)
}

func (a *App) shutdown(started []Listener) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		listener := started[i]
		a.logger.Info("stopping listener", "listener", listener.Name())
		if err := listener.Shutdown(shutdownCtx); err != nil {
			wrapped := fmt.Errorf("shutdown listener %q: %w", listener.Name(), err)
			errs = append(errs, wrapped)
			a.logger.Error("listener shutdown failed", "listener", listener.Name(), "error", err)
			continue
		}
		a.logger.Info("listener stopped", "listener", listener.Name())
	}
	return errors.Join(errs...)
}

package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

type Listener struct {
	name        string
	factory     botFactory
	application ports.MeetingApplication
	logger      *slog.Logger

	mu      sync.Mutex
	started bool
	api     botAPI
	done    chan struct{}
}

func New(name, token string, application ports.MeetingApplication, logger *slog.Logger) (*Listener, error) {
	return newListener(name, func() (botAPI, error) {
		return newTelebotAdapter(token, logger)
	}, application, logger)
}

func newListener(name string, factory botFactory, application ports.MeetingApplication, logger *slog.Logger) (*Listener, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("Telegram listener name is required")
	}
	if factory == nil {
		return nil, errors.New("Telegram bot factory is required")
	}
	if application == nil {
		return nil, errors.New("meeting application is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Listener{name: name, factory: factory, application: application, logger: logger}, nil
}

func (l *Listener) Name() string { return "telegram:" + l.name }

func (l *Listener) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return fmt.Errorf("%w: Telegram listener %q is already started", domain.ErrConflict, l.name)
	}
	api, err := l.factory()
	if err != nil {
		return fmt.Errorf("initialize Telegram listener %q: %w", l.name, err)
	}
	handler, err := NewHandler(l.application, api, l.logger)
	if err != nil {
		return fmt.Errorf("build Telegram listener %q handler: %w", l.name, err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	api.Handle(func(message Message) {
		l.handleMessage(runCtx, api, handler, message)
	})

	l.started = true
	l.api = api
	l.done = make(chan struct{})
	done := l.done
	go func() {
		defer close(done)
		defer cancel()
		api.Start()
	}()
	l.logger.Info("Telegram listener started", "listener", l.name)
	return nil
}

func (l *Listener) Shutdown(ctx context.Context) error {
	l.mu.Lock()
	if !l.started {
		l.mu.Unlock()
		return nil
	}
	api := l.api
	done := l.done
	l.mu.Unlock()

	stopDone := make(chan struct{})
	go func() {
		api.Stop()
		close(stopDone)
	}()
	if err := waitFor(ctx, stopDone, l.name, "stop"); err != nil {
		return err
	}
	if err := waitFor(ctx, done, l.name, "finish"); err != nil {
		return err
	}

	l.mu.Lock()
	if l.done == done {
		l.started = false
		l.api = nil
		l.done = nil
	}
	l.mu.Unlock()
	l.logger.Info("Telegram listener stopped", "listener", l.name)
	return nil
}

func (l *Listener) handleMessage(ctx context.Context, api botAPI, handler *Handler, message Message) {
	if message.Voice != nil || message.Audio != nil || (message.File != nil && isAudioMIME(message.File.MIMEType)) {
		if err := api.SendMessage(ctx, message.Chat.ID, "Файл получен, загружаю и ставлю в очередь обработки…"); err != nil && ctx.Err() == nil {
			l.logger.Warn("send Telegram upload acknowledgement", "listener", l.name, "error", err)
		}
	}

	for _, reply := range handler.Handle(ctx, message) {
		if err := api.SendMessage(ctx, message.Chat.ID, reply); err != nil {
			if ctx.Err() == nil {
				l.logger.Error("send Telegram reply", "listener", l.name, "error", err)
			}
			return
		}
	}
}

func waitFor(ctx context.Context, done <-chan struct{}, listenerName, operation string) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s Telegram listener %q: %w", operation, listenerName, ctx.Err())
	}
}

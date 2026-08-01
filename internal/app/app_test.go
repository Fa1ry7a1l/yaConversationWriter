package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"yaConversationWriter/internal/app"
	"yaConversationWriter/internal/config"
	llmmock "yaConversationWriter/internal/infrastructure/llm/mock"
	"yaConversationWriter/internal/infrastructure/repository/memory"
	speechmock "yaConversationWriter/internal/infrastructure/speech/mock"
)

func TestRunStartsAndStopsListenersInLifecycleOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := &eventRecorder{}
	first := &fakeListener{name: "first", events: recorder}
	second := &fakeListener{name: "second", events: recorder, onStart: cancel}
	application := newTestApp(t, first, second)

	if err := application.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"start:first", "start:second", "shutdown:second", "shutdown:first"}
	if got := recorder.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRunStopsAlreadyStartedListenersOnStartFailure(t *testing.T) {
	startErr := errors.New("cannot start")
	recorder := &eventRecorder{}
	first := &fakeListener{name: "first", events: recorder}
	second := &fakeListener{name: "second", events: recorder, startErr: startErr}
	application := newTestApp(t, first, second)

	err := application.Run(context.Background())
	if !errors.Is(err, startErr) {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"start:first", "start:second", "shutdown:first"}
	if got := recorder.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRunAttemptsEveryShutdownAndReturnsErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdownErr := errors.New("cannot stop")
	recorder := &eventRecorder{}
	first := &fakeListener{name: "first", events: recorder, shutdownErr: shutdownErr}
	second := &fakeListener{name: "second", events: recorder, onStart: cancel}
	application := newTestApp(t, first, second)

	err := application.Run(ctx)
	if !errors.Is(err, shutdownErr) {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"start:first", "start:second", "shutdown:second", "shutdown:first"}
	if got := recorder.snapshot(); !equalStrings(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestBuildRejectsEnabledTelegramUntilAdapterIncrement(t *testing.T) {
	cfg := config.Default()
	cfg.Listeners = []config.Listener{{Name: "primary", Type: config.ListenerTelegram, Enabled: true, Token: "secret"}}
	_, err := app.Build(cfg, testLogger())
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("Build() error = %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("Build() leaked a secret: %v", err)
	}
}

func newTestApp(t *testing.T, listeners ...app.Listener) *app.App {
	t.Helper()
	application, err := app.New(testLogger(), time.Second, app.Dependencies{
		Repository: memory.New(),
		Speech:     speechmock.New(speechmock.Config{Transcript: "transcript"}),
		LLM:        llmmock.New(llmmock.Config{Summary: "summary", Answer: "answer"}),
	}, listeners...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return application
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeListener struct {
	name        string
	events      *eventRecorder
	startErr    error
	shutdownErr error
	onStart     func()
}

func (f *fakeListener) Name() string { return f.name }

func (f *fakeListener) Start(context.Context) error {
	f.events.add("start:" + f.name)
	if f.onStart != nil {
		f.onStart()
	}
	return f.startErr
}

func (f *fakeListener) Shutdown(context.Context) error {
	f.events.add("shutdown:" + f.name)
	return f.shutdownErr
}

type eventRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *eventRecorder) add(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *eventRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

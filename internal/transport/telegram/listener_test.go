package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

func TestListenerAcknowledgesUploadBeforeDownloadingAndDispatching(t *testing.T) {
	events := &listenerEvents{}
	api := newFakeBotAPI(events)
	api.message = &Message{
		Chat:  Chat{ID: 7},
		From:  &User{ID: 42},
		Voice: &VoiceFile{FileID: "voice-file", MIMEType: "audio/ogg"},
	}
	api.content = []byte("audio")
	application := &fakeApplication{
		uploadResult: ports.UploadMeetingResult{
			Meeting: domain.Meeting{ID: "meeting-1"},
			Job:     domain.ProcessingJob{Status: domain.StatusCreated},
		},
		uploadHook: func() { events.add("upload") },
	}
	listener, err := newListener("primary", func() (botAPI, error) { return api, nil }, application, testTelegramLogger())
	if err != nil {
		t.Fatalf("newListener() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-api.finalMessage:
	case <-time.After(time.Second):
		t.Fatal("listener did not send final reply")
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := listener.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	want := []string{
		"handle",
		"start",
		"send:Файл получен",
		"download:voice-file",
		"upload",
		"send:Файл принят",
		"stop",
	}
	if got := events.snapshot(); !equalEventPrefixes(got, want) {
		t.Fatalf("events = %v, want prefixes %v", got, want)
	}
	if application.uploadRequest.ExternalUserID != "telegram:42" || string(application.uploadRequest.Content) != "audio" {
		t.Fatalf("upload request = %+v", application.uploadRequest)
	}
}

func TestListenerStartReturnsFactoryError(t *testing.T) {
	authErr := errors.New("unauthorized")
	listener, err := newListener("primary", func() (botAPI, error) { return nil, authErr }, &fakeApplication{}, testTelegramLogger())
	if err != nil {
		t.Fatalf("newListener() error = %v", err)
	}

	err = listener.Start(context.Background())
	if !errors.Is(err, authErr) || !strings.Contains(err.Error(), "primary") {
		t.Fatalf("Start() error = %v", err)
	}
	if err := listener.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() after failed start error = %v", err)
	}
}

func TestListenerRejectsDuplicateStart(t *testing.T) {
	api := newFakeBotAPI(nil)
	listener, err := newListener("primary", func() (botAPI, error) { return api, nil }, &fakeApplication{}, testTelegramLogger())
	if err != nil {
		t.Fatalf("newListener() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-api.started:
	case <-time.After(time.Second):
		t.Fatal("fake bot did not start")
	}
	if err := listener.Start(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate Start() error = %v", err)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := listener.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

type fakeBotAPI struct {
	mu sync.Mutex

	events       *listenerEvents
	handler      func(Message)
	message      *Message
	content      []byte
	started      chan struct{}
	stop         chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
	finalMessage chan struct{}
	finalOnce    sync.Once
}

func newFakeBotAPI(events *listenerEvents) *fakeBotAPI {
	return &fakeBotAPI{
		events:       events,
		started:      make(chan struct{}),
		stop:         make(chan struct{}),
		finalMessage: make(chan struct{}),
	}
}

func (f *fakeBotAPI) Handle(handler func(Message)) {
	f.record("handle")
	f.mu.Lock()
	f.handler = handler
	f.mu.Unlock()
}

func (f *fakeBotAPI) Start() {
	f.record("start")
	f.startOnce.Do(func() { close(f.started) })
	f.mu.Lock()
	handler, message := f.handler, f.message
	f.mu.Unlock()
	if handler != nil && message != nil {
		handler(*message)
	}
	<-f.stop
}

func (f *fakeBotAPI) Stop() {
	f.record("stop")
	f.stopOnce.Do(func() { close(f.stop) })
}

func (f *fakeBotAPI) SendMessage(_ context.Context, _ int64, text string) error {
	f.record("send:" + text)
	if strings.Contains(text, "Файл принят") {
		f.finalOnce.Do(func() { close(f.finalMessage) })
	}
	return nil
}

func (f *fakeBotAPI) DownloadFile(_ context.Context, fileID string) ([]byte, error) {
	f.record("download:" + fileID)
	return append([]byte(nil), f.content...), nil
}

func (f *fakeBotAPI) record(event string) {
	if f.events != nil {
		f.events.add(event)
	}
}

type listenerEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *listenerEvents) add(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *listenerEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

func equalEventPrefixes(events, prefixes []string) bool {
	if len(events) != len(prefixes) {
		return false
	}
	for index := range prefixes {
		if !strings.HasPrefix(events[index], prefixes[index]) {
			return false
		}
	}
	return true
}

func testTelegramLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

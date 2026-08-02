//go:build integration

package postgres

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"yaConversationWriter/internal/domain"
	llmmock "yaConversationWriter/internal/infrastructure/llm/mock"
	speechmock "yaConversationWriter/internal/infrastructure/speech/mock"
	"yaConversationWriter/internal/service"
	"yaConversationWriter/internal/transport/telegram"
	"yaConversationWriter/internal/worker"
)

func TestTelegramApplicationWorkerPostgresFullFlow(t *testing.T) {
	repository := resetIntegrationRepository(t)
	logger := integrationLogger()
	speech := speechmock.New(speechmock.Config{Transcript: "launch plan and next steps"})
	llm := llmmock.New(llmmock.Config{Summary: "launch summary", Answer: "prepared answer"})
	processor, err := service.NewProcessor(repository, speech, llm, logger)
	if err != nil {
		t.Fatalf("service.NewProcessor() error = %v", err)
	}
	workers, err := worker.New(worker.Config{Count: 1, QueueSize: 2, TaskTimeout: time.Second}, processor, logger)
	if err != nil {
		t.Fatalf("worker.New() error = %v", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	if err := workers.Start(runCtx); err != nil {
		cancel()
		t.Fatalf("workers.Start() error = %v", err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		if err := workers.Shutdown(shutdownCtx); err != nil {
			t.Errorf("workers.Shutdown() error = %v", err)
		}
	})

	application, err := service.NewMeetings(repository, llm, workers, logger)
	if err != nil {
		t.Fatalf("service.NewMeetings() error = %v", err)
	}
	handler, err := telegram.NewHandler(application, integrationDownloader{content: []byte("audio bytes")}, logger)
	if err != nil {
		t.Fatalf("telegram.NewHandler() error = %v", err)
	}
	message := func(userID int64, text string) telegram.Message {
		return telegram.Message{
			Chat: telegram.Chat{ID: userID, Type: "private"},
			From: &telegram.User{ID: userID},
			Text: text,
		}
	}

	assertRepliesContain(t, handler.Handle(runCtx, message(1001, "/start")), "Вы зарегистрированы")
	uploadReplies := handler.Handle(runCtx, telegram.Message{
		Chat:  telegram.Chat{ID: 1001, Type: "private"},
		From:  &telegram.User{ID: 1001},
		Voice: &telegram.VoiceFile{FileID: "e2e-voice", Duration: 5, MIMEType: "audio/ogg"},
	})
	assertRepliesContain(t, uploadReplies, "Файл принят")

	records, err := application.ListMeetings(runCtx, "telegram:1001")
	if err != nil || len(records) != 1 {
		t.Fatalf("ListMeetings() records=%+v error=%v", records, err)
	}
	meetingID := records[0].Meeting.ID
	waitForCompleted(t, application, meetingID)

	assertRepliesContain(t, handler.Handle(runCtx, message(1001, "list")), "launch summary")
	assertRepliesContain(t, handler.Handle(runCtx, message(1001, "status "+string(meetingID))), "Статус: completed")
	assertRepliesContain(t, handler.Handle(runCtx, message(1001, "get "+string(meetingID))), "launch plan and next steps")
	assertRepliesContain(t, handler.Handle(runCtx, message(1001, "find launch")), string(meetingID))
	assertRepliesContain(t, handler.Handle(runCtx, message(1001, "chat "+string(meetingID)+" What was decided?")), "prepared answer")

	assertRepliesContain(t, handler.Handle(runCtx, message(2002, "/start")), "Вы зарегистрированы")
	assertRepliesContain(t, handler.Handle(runCtx, message(2002, "get "+string(meetingID))), "Встреча не найдена")
}

func waitForCompleted(t *testing.T, application interface {
	GetStatus(context.Context, string, domain.MeetingID) (domain.ProcessingJob, error)
}, meetingID domain.MeetingID) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err := application.GetStatus(context.Background(), "telegram:1001", meetingID)
		if err == nil && job.Status == domain.StatusCompleted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("meeting %s did not complete: job=%+v error=%v", meetingID, job, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertRepliesContain(t *testing.T, replies []string, expected string) {
	t.Helper()
	if !strings.Contains(strings.Join(replies, "\n"), expected) {
		t.Fatalf("replies=%q, want substring %q", replies, expected)
	}
}

type integrationDownloader struct {
	content []byte
}

func (d integrationDownloader) DownloadFile(context.Context, string) ([]byte, error) {
	return append([]byte(nil), d.content...), nil
}

func integrationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

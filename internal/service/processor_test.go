package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"yaConversationWriter/internal/domain"
	llmmock "yaConversationWriter/internal/infrastructure/llm/mock"
	"yaConversationWriter/internal/infrastructure/repository/memory"
	speechmock "yaConversationWriter/internal/infrastructure/speech/mock"
	"yaConversationWriter/internal/ports"
	"yaConversationWriter/internal/service"
	"yaConversationWriter/internal/worker"
)

func TestProcessorCompletesMeeting(t *testing.T) {
	repository, task := processingFixture(t)
	speechClient := speechmock.New(speechmock.Config{Transcript: "launch transcript"})
	llmClient := llmmock.New(llmmock.Config{Summary: "launch summary"})
	processor := newProcessor(t, repository, speechClient, llmClient)

	if err := processor.Process(context.Background(), task); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	job, err := repository.GetJob(context.Background(), task.UserID, task.MeetingID)
	if err != nil || job.Status != domain.StatusCompleted {
		t.Fatalf("job=%+v error=%v", job, err)
	}
	transcript, err := repository.GetTranscript(context.Background(), task.UserID, task.MeetingID)
	if err != nil || transcript.Text != "launch transcript" {
		t.Fatalf("transcript=%+v error=%v", transcript, err)
	}
	summary, err := repository.GetSummary(context.Background(), task.UserID, task.MeetingID)
	if err != nil || summary.Text != "launch summary" {
		t.Fatalf("summary=%+v error=%v", summary, err)
	}
	if calls := llmClient.SummaryCalls(); len(calls) != 1 || calls[0].Transcript != transcript.Text {
		t.Fatalf("summary calls = %+v", calls)
	}
}

func TestProcessorSpeechFailureSkipsLLMAndPersistsFailure(t *testing.T) {
	repository, task := processingFixture(t)
	speechErr := errors.New("speech unavailable")
	speechClient := speechmock.New(speechmock.Config{Err: speechErr})
	llmClient := llmmock.New(llmmock.Config{Summary: "must not be used"})
	processor := newProcessor(t, repository, speechClient, llmClient)

	err := processor.Process(context.Background(), task)
	if !errors.Is(err, speechErr) {
		t.Fatalf("Process() error = %v", err)
	}
	if len(llmClient.SummaryCalls()) != 0 {
		t.Fatalf("LLM was called after speech failure: %+v", llmClient.SummaryCalls())
	}
	assertFailedJob(t, repository, task, "speech unavailable")
}

func TestProcessorLLMFailurePersistsFailureAfterTranscript(t *testing.T) {
	repository, task := processingFixture(t)
	llmErr := errors.New("LLM unavailable")
	processor := newProcessor(t, repository,
		speechmock.New(speechmock.Config{Transcript: "transcript"}),
		llmmock.New(llmmock.Config{SummaryErr: llmErr}),
	)

	err := processor.Process(context.Background(), task)
	if !errors.Is(err, llmErr) {
		t.Fatalf("Process() error = %v", err)
	}
	if _, err := repository.GetTranscript(context.Background(), task.UserID, task.MeetingID); err != nil {
		t.Fatalf("transcript was not saved before LLM failure: %v", err)
	}
	assertFailedJob(t, repository, task, "LLM unavailable")
}

func TestProcessorPersistsFailureWithCanceledTaskContext(t *testing.T) {
	repository, task := processingFixture(t)
	processor := newProcessor(t, repository,
		speechmock.New(speechmock.Config{Transcript: "transcript"}),
		llmmock.New(llmmock.Config{Summary: "summary"}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := processor.Process(ctx, task)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Process() error = %v", err)
	}
	assertFailedJob(t, repository, task, "context canceled")
}

func TestWorkerTimeoutIsPersistedAsFailed(t *testing.T) {
	repository, task := processingFixture(t)
	processor := newProcessor(t, repository,
		speechmock.New(speechmock.Config{Transcript: "late", Delay: time.Hour}),
		llmmock.New(llmmock.Config{Summary: "summary"}),
	)
	pool, err := worker.New(worker.Config{Count: 1, QueueSize: 1, TaskTimeout: 15 * time.Millisecond}, processor, testLogger())
	if err != nil {
		t.Fatalf("worker.New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := pool.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		job, getErr := repository.GetJob(context.Background(), task.UserID, task.MeetingID)
		if getErr == nil && job.Status == domain.StatusFailed {
			if !strings.Contains(job.Error, "deadline exceeded") {
				t.Fatalf("failed job error = %q", job.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not fail before deadline: job=%+v error=%v", job, getErr)
		}
		time.Sleep(time.Millisecond)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := pool.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestWorkerShutdownPersistsActiveAndQueuedTasksAsFailed(t *testing.T) {
	repository, activeTask := processingFixture(t)
	queuedTask := addMeetingTask(t, repository, activeTask.UserID, "file-2")
	processor := newProcessor(t, repository,
		speechmock.New(speechmock.Config{Transcript: "late", Delay: time.Hour}),
		llmmock.New(llmmock.Config{Summary: "summary"}),
	)
	pool, err := worker.New(worker.Config{Count: 1, QueueSize: 2, TaskTimeout: time.Hour}, processor, testLogger())
	if err != nil {
		t.Fatalf("worker.New() error = %v", err)
	}
	if err := pool.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := pool.Enqueue(context.Background(), activeTask); err != nil {
		t.Fatalf("enqueue active task: %v", err)
	}
	waitForStatus(t, repository, activeTask, domain.StatusProcessing)
	if err := pool.Enqueue(context.Background(), queuedTask); err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	assertFailedJob(t, repository, activeTask, "context canceled")
	assertFailedJob(t, repository, queuedTask, "context canceled")
}

func processingFixture(t *testing.T) (*memory.Repository, ports.ProcessingTask) {
	t.Helper()
	repository := memory.New()
	user, err := repository.GetOrCreateUser(context.Background(), "telegram:owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	file := domain.FileMetadata{ExternalFileID: "file-1", FileName: "meeting.ogg", MIMEType: "audio/ogg"}
	meeting, _, err := repository.CreateMeetingWithJob(context.Background(), user.ID, file)
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}
	return repository, ports.ProcessingTask{
		UserID:    user.ID,
		MeetingID: meeting.ID,
		Audio: ports.AudioInput{
			MeetingID: meeting.ID,
			File:      file,
			Content:   []byte("audio"),
		},
	}
}

func addMeetingTask(t *testing.T, repository *memory.Repository, userID domain.UserID, fileID string) ports.ProcessingTask {
	t.Helper()
	file := domain.FileMetadata{ExternalFileID: fileID, FileName: fileID + ".ogg", MIMEType: "audio/ogg"}
	meeting, _, err := repository.CreateMeetingWithJob(context.Background(), userID, file)
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}
	return ports.ProcessingTask{
		UserID:    userID,
		MeetingID: meeting.ID,
		Audio:     ports.AudioInput{MeetingID: meeting.ID, File: file, Content: []byte("audio")},
	}
}

func waitForStatus(t *testing.T, repository *memory.Repository, task ports.ProcessingTask, status domain.JobStatus) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		job, err := repository.GetJob(context.Background(), task.UserID, task.MeetingID)
		if err == nil && job.Status == status {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not reach %s: job=%+v error=%v", status, job, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func newProcessor(t *testing.T, repository ports.ProcessingRepository, speech ports.SpeechClient, llm ports.LLMClient) *service.Processor {
	t.Helper()
	processor, err := service.NewProcessor(repository, speech, llm, testLogger())
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	return processor
}

func assertFailedJob(t *testing.T, repository *memory.Repository, task ports.ProcessingTask, contains string) {
	t.Helper()
	job, err := repository.GetJob(context.Background(), task.UserID, task.MeetingID)
	if err != nil || job.Status != domain.StatusFailed || !strings.Contains(job.Error, contains) {
		t.Fatalf("failed job=%+v error=%v", job, err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

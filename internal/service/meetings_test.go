package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"yaConversationWriter/internal/domain"
	llmmock "yaConversationWriter/internal/infrastructure/llm/mock"
	"yaConversationWriter/internal/infrastructure/repository/memory"
	speechmock "yaConversationWriter/internal/infrastructure/speech/mock"
	"yaConversationWriter/internal/ports"
	"yaConversationWriter/internal/service"
	"yaConversationWriter/internal/worker"
)

func TestAsynchronousMeetingFlow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repository := memory.New()
		speechClient := speechmock.New(speechmock.Config{Transcript: "async transcript"})
		llmClient := llmmock.New(llmmock.Config{Summary: "async summary", Answer: "answer"})
		processor := newProcessor(t, repository, speechClient, llmClient)
		pool, err := worker.New(worker.Config{Count: 1, QueueSize: 2, TaskTimeout: time.Second}, processor, testLogger())
		if err != nil {
			t.Fatalf("worker.New() error = %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := pool.Start(ctx); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		meetings := newMeetings(t, repository, llmClient, pool)
		upload, err := meetings.UploadMeeting(ctx, ports.UploadMeetingRequest{
			ExternalUserID: "telegram:owner",
			File:           domain.FileMetadata{ExternalFileID: "async-file", MIMEType: "audio/ogg"},
			Content:        []byte("audio"),
		})
		if err != nil || upload.Job.Status != domain.StatusCreated {
			t.Fatalf("UploadMeeting() result=%+v error=%v", upload, err)
		}

		synctest.Wait()
		job, err := meetings.GetStatus(context.Background(), "telegram:owner", upload.Meeting.ID)
		if err != nil || job.Status != domain.StatusCompleted {
			t.Fatalf("meeting did not complete: job=%+v error=%v", job, err)
		}
		transcript, err := meetings.GetTranscript(context.Background(), "telegram:owner", upload.Meeting.ID)
		if err != nil || transcript.Text != "async transcript" {
			t.Fatalf("GetTranscript() transcript=%+v error=%v", transcript, err)
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		if err := pool.Shutdown(shutdownCtx); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	})
}

func TestMeetingsUploadAndMandatoryUseCases(t *testing.T) {
	ctx := context.Background()
	repository := memory.New()
	dispatcher := &captureDispatcher{}
	llmClient := llmmock.New(llmmock.Config{Summary: "planning summary", Answer: "planned answer"})
	meetings := newMeetings(t, repository, llmClient, dispatcher)
	if _, err := meetings.RegisterUser(ctx, "telegram:other"); err != nil {
		t.Fatalf("register other user: %v", err)
	}

	content := []byte("audio bytes")
	upload, err := meetings.UploadMeeting(ctx, ports.UploadMeetingRequest{
		ExternalUserID: "telegram:owner",
		File: domain.FileMetadata{
			ExternalFileID: "file-1",
			FileName:       "planning.ogg",
			MIMEType:       "audio/ogg",
			SizeBytes:      int64(len(content)),
		},
		Content: content,
	})
	if err != nil || upload.Job.Status != domain.StatusCreated {
		t.Fatalf("UploadMeeting() result=%+v error=%v", upload, err)
	}
	content[0] = 'X'
	task := dispatcher.singleTask(t)
	if string(task.Audio.Content) != "audio bytes" || task.MeetingID != upload.Meeting.ID {
		t.Fatalf("dispatched task = %+v", task)
	}

	processor := newProcessor(t, repository,
		speechmock.New(speechmock.Config{Transcript: "planning transcript with launch keyword"}),
		llmClient,
	)
	if err := processor.Process(ctx, task); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	list, err := meetings.ListMeetings(ctx, "telegram:owner")
	if err != nil || len(list) != 1 || list[0].Meeting.ID != upload.Meeting.ID || list[0].Summary == nil {
		t.Fatalf("ListMeetings() records=%+v error=%v", list, err)
	}
	status, err := meetings.GetStatus(ctx, "telegram:owner", upload.Meeting.ID)
	if err != nil || status.Status != domain.StatusCompleted {
		t.Fatalf("GetStatus() status=%+v error=%v", status, err)
	}
	transcript, err := meetings.GetTranscript(ctx, "telegram:owner", upload.Meeting.ID)
	if err != nil || transcript.Text == "" {
		t.Fatalf("GetTranscript() transcript=%+v error=%v", transcript, err)
	}
	results, err := meetings.FindMeetings(ctx, "telegram:owner", "launch")
	if err != nil || len(results) != 1 || results[0].Record.Meeting.ID != upload.Meeting.ID {
		t.Fatalf("FindMeetings() results=%+v error=%v", results, err)
	}
	answer, err := meetings.Chat(ctx, "telegram:owner", upload.Meeting.ID, "What is planned?")
	if err != nil || answer != "planned answer" {
		t.Fatalf("Chat() answer=%q error=%v", answer, err)
	}
	answerCalls := llmClient.AnswerCalls()
	if len(answerCalls) != 1 || answerCalls[0].MeetingID != upload.Meeting.ID || answerCalls[0].Transcript != transcript.Text {
		t.Fatalf("answer calls = %+v", answerCalls)
	}

	assertMeetingHidden(t, meetings, "telegram:other", upload.Meeting.ID)
}

func TestMeetingsRejectsUnsupportedUploadWithoutCreatingUser(t *testing.T) {
	repository := memory.New()
	meetings := newMeetings(t, repository, llmmock.New(llmmock.Config{Answer: "answer"}), &captureDispatcher{})
	_, err := meetings.UploadMeeting(context.Background(), ports.UploadMeetingRequest{
		ExternalUserID: "telegram:owner",
		File:           domain.FileMetadata{ExternalFileID: "file", MIMEType: "text/plain"},
		Content:        []byte("not audio"),
	})
	if !errors.Is(err, domain.ErrUnsupportedFile) {
		t.Fatalf("UploadMeeting() error = %v", err)
	}
	if _, err := repository.GetUserByExternalID(context.Background(), "telegram:owner"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("invalid upload created user, lookup error = %v", err)
	}
}

func TestMeetingsPersistsQueueFailure(t *testing.T) {
	repository := memory.New()
	dispatcher := &captureDispatcher{err: domain.ErrQueueFull}
	meetings := newMeetings(t, repository, llmmock.New(llmmock.Config{Answer: "answer"}), dispatcher)
	result, err := meetings.UploadMeeting(context.Background(), ports.UploadMeetingRequest{
		ExternalUserID: "telegram:owner",
		File:           domain.FileMetadata{ExternalFileID: "file", MIMEType: "audio/ogg"},
		Content:        []byte("audio"),
	})
	if !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("UploadMeeting() error = %v", err)
	}
	if result.Job.Status != domain.StatusFailed || result.Job.Error == "" {
		t.Fatalf("result job = %+v", result.Job)
	}
	stored, getErr := meetings.GetStatus(context.Background(), "telegram:owner", result.Meeting.ID)
	if getErr != nil || stored.Status != domain.StatusFailed {
		t.Fatalf("stored job=%+v error=%v", stored, getErr)
	}
}

func TestMeetingsChatRequiresCompletedMeeting(t *testing.T) {
	repository := memory.New()
	dispatcher := &captureDispatcher{}
	meetings := newMeetings(t, repository, llmmock.New(llmmock.Config{Answer: "answer"}), dispatcher)
	upload, err := meetings.UploadMeeting(context.Background(), ports.UploadMeetingRequest{
		ExternalUserID: "telegram:owner",
		File:           domain.FileMetadata{ExternalFileID: "file", MIMEType: "audio/ogg"},
		Content:        []byte("audio"),
	})
	if err != nil {
		t.Fatalf("UploadMeeting() error = %v", err)
	}
	if _, err := meetings.Chat(context.Background(), "telegram:owner", upload.Meeting.ID, "question"); !errors.Is(err, domain.ErrNotReady) {
		t.Fatalf("Chat() error = %v", err)
	}
}

func newMeetings(t *testing.T, repository ports.Repository, llm ports.LLMClient, dispatcher ports.JobDispatcher) *service.Meetings {
	t.Helper()
	meetings, err := service.NewMeetings(repository, llm, dispatcher, testLogger())
	if err != nil {
		t.Fatalf("NewMeetings() error = %v", err)
	}
	return meetings
}

func assertMeetingHidden(t *testing.T, meetings *service.Meetings, externalUserID string, meetingID domain.MeetingID) {
	t.Helper()
	ctx := context.Background()
	checks := []func() error{
		func() error { _, err := meetings.GetStatus(ctx, externalUserID, meetingID); return err },
		func() error { _, err := meetings.GetTranscript(ctx, externalUserID, meetingID); return err },
		func() error { _, err := meetings.Chat(ctx, externalUserID, meetingID, "question"); return err },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ownership check %d error = %v", i, err)
		}
	}
	results, err := meetings.FindMeetings(ctx, externalUserID, "launch")
	if err != nil || len(results) != 0 {
		t.Fatalf("other user search results=%+v error=%v", results, err)
	}
	records, err := meetings.ListMeetings(ctx, externalUserID)
	if err != nil || len(records) != 0 {
		t.Fatalf("other user meeting list=%+v error=%v", records, err)
	}
}

type captureDispatcher struct {
	mu    sync.Mutex
	tasks []ports.ProcessingTask
	err   error
}

func (d *captureDispatcher) Enqueue(_ context.Context, task ports.ProcessingTask) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	task.Audio.Content = append([]byte(nil), task.Audio.Content...)
	d.tasks = append(d.tasks, task)
	return d.err
}

func (d *captureDispatcher) singleTask(t *testing.T) ports.ProcessingTask {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.tasks) != 1 {
		t.Fatalf("dispatched tasks = %d", len(d.tasks))
	}
	return d.tasks[0]
}

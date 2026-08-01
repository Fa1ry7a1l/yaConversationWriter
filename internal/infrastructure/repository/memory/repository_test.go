package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/infrastructure/repository/memory"
)

func TestRepositoryProcessingFlowAndOwnerIsolation(t *testing.T) {
	ctx := context.Background()
	repository := memory.New()
	owner, err := repository.GetOrCreateUser(ctx, "telegram:100")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := repository.GetOrCreateUser(ctx, "telegram:200")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	meeting, job, err := repository.CreateMeetingWithJob(ctx, owner.ID, domain.FileMetadata{
		ExternalFileID: "telegram-file-1",
		FileName:       "planning.ogg",
		MIMEType:       "audio/ogg",
	})
	if err != nil {
		t.Fatalf("CreateMeetingWithJob() error = %v", err)
	}
	if job.Status != domain.StatusCreated || job.MeetingID != meeting.ID {
		t.Fatalf("unexpected initial job: %+v", job)
	}

	job, err = repository.MarkProcessing(ctx, owner.ID, meeting.ID)
	if err != nil || job.Status != domain.StatusProcessing {
		t.Fatalf("MarkProcessing() job=%+v error=%v", job, err)
	}
	transcript, job, err := repository.SaveTranscript(ctx, owner.ID, meeting.ID, "Discuss the launch schedule and risks")
	if err != nil || job.Status != domain.StatusTranscribed || transcript.Text == "" {
		t.Fatalf("SaveTranscript() transcript=%+v job=%+v error=%v", transcript, job, err)
	}
	summary, job, err := repository.SaveSummary(ctx, owner.ID, meeting.ID, "Launch planning summary")
	if err != nil || job.Status != domain.StatusSummarized || summary.Text == "" {
		t.Fatalf("SaveSummary() summary=%+v job=%+v error=%v", summary, job, err)
	}
	job, err = repository.MarkCompleted(ctx, owner.ID, meeting.ID)
	if err != nil || job.Status != domain.StatusCompleted {
		t.Fatalf("MarkCompleted() job=%+v error=%v", job, err)
	}

	results, err := repository.FindMeetings(ctx, owner.ID, "schedule")
	if err != nil || len(results) != 1 || results[0].Record.Meeting.ID != meeting.ID {
		t.Fatalf("FindMeetings() results=%+v error=%v", results, err)
	}
	list, err := repository.ListMeetings(ctx, owner.ID)
	if err != nil || len(list) != 1 || list[0].Summary == nil {
		t.Fatalf("ListMeetings() records=%+v error=%v", list, err)
	}

	assertHiddenFromUser(t, repository, other.ID, meeting.ID)
}

func TestRepositoryFailureAndInvalidTransition(t *testing.T) {
	ctx := context.Background()
	repository := memory.New()
	user, _ := repository.GetOrCreateUser(ctx, "telegram:100")
	meeting, _, _ := repository.CreateMeetingWithJob(ctx, user.ID, domain.FileMetadata{ExternalFileID: "file"})

	_, _, err := repository.SaveTranscript(ctx, user.ID, meeting.ID, "too early")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("SaveTranscript() error = %v", err)
	}
	job, err := repository.MarkFailed(ctx, user.ID, meeting.ID, "speech failed")
	if err != nil || job.Status != domain.StatusFailed || job.Error != "speech failed" {
		t.Fatalf("MarkFailed() job=%+v error=%v", job, err)
	}
	stored, err := repository.GetJob(ctx, user.ID, meeting.ID)
	if err != nil || stored.Status != domain.StatusFailed || stored.Error != "speech failed" {
		t.Fatalf("GetJob() job=%+v error=%v", stored, err)
	}
}

func TestRepositoryConcurrentGetOrCreateUser(t *testing.T) {
	repository := memory.New()
	const goroutines = 64

	ids := make(chan domain.UserID, goroutines)
	errs := make(chan error, goroutines)
	var group sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			user, err := repository.GetOrCreateUser(context.Background(), "telegram:shared")
			if err != nil {
				errs <- err
				return
			}
			ids <- user.ID
		}()
	}
	group.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Errorf("GetOrCreateUser() error = %v", err)
	}
	var expected domain.UserID
	for id := range ids {
		if expected == "" {
			expected = id
		}
		if id != expected {
			t.Fatalf("different IDs returned: %q and %q", expected, id)
		}
	}
}

func TestRepositoryHonorsCanceledContext(t *testing.T) {
	repository := memory.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repository.GetOrCreateUser(ctx, "telegram:100")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrCreateUser() error = %v", err)
	}
}

func assertHiddenFromUser(t *testing.T, repository *memory.Repository, userID domain.UserID, meetingID domain.MeetingID) {
	t.Helper()
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{name: "meeting", call: func() error { _, err := repository.GetMeeting(ctx, userID, meetingID); return err }},
		{name: "job", call: func() error { _, err := repository.GetJob(ctx, userID, meetingID); return err }},
		{name: "transcript", call: func() error { _, err := repository.GetTranscript(ctx, userID, meetingID); return err }},
		{name: "summary", call: func() error { _, err := repository.GetSummary(ctx, userID, meetingID); return err }},
	}
	for _, check := range checks {
		t.Run(fmt.Sprintf("other user cannot read %s", check.name), func(t *testing.T) {
			if err := check.call(); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	results, err := repository.FindMeetings(ctx, userID, "schedule")
	if err != nil || len(results) != 0 {
		t.Fatalf("other user search results=%+v error=%v", results, err)
	}
}

package domain_test

import (
	"errors"
	"testing"
	"time"

	"yaConversationWriter/internal/domain"
)

func TestProcessingJobTransition(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	job := domain.ProcessingJob{Status: domain.StatusCreated, CreatedAt: now, UpdatedAt: now}

	steps := []domain.JobStatus{
		domain.StatusProcessing,
		domain.StatusTranscribed,
		domain.StatusSummarized,
		domain.StatusCompleted,
	}
	for i, status := range steps {
		var err error
		job, err = job.Transition(status, now.Add(time.Duration(i+1)*time.Second), "")
		if err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	if job.CompletedAt == nil || job.StartedAt == nil {
		t.Fatalf("lifecycle timestamps were not set: %+v", job)
	}
}

func TestProcessingJobRejectsInvalidTransitionAndEmptyFailure(t *testing.T) {
	now := time.Now()
	job := domain.ProcessingJob{Status: domain.StatusCreated}
	_, err := job.Transition(domain.StatusCompleted, now, "")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
	_, err = job.Transition(domain.StatusFailed, now, "")
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("empty failure error = %v", err)
	}
}

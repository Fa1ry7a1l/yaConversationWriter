package domain

import (
	"fmt"
	"strings"
	"time"
)

type JobStatus string

const (
	StatusCreated     JobStatus = "created"
	StatusProcessing  JobStatus = "processing"
	StatusTranscribed JobStatus = "transcribed"
	StatusSummarized  JobStatus = "summarized"
	StatusCompleted   JobStatus = "completed"
	StatusFailed      JobStatus = "failed"
)

func (s JobStatus) Valid() bool {
	switch s {
	case StatusCreated, StatusProcessing, StatusTranscribed, StatusSummarized, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

func (s JobStatus) CanTransitionTo(next JobStatus) bool {
	if next == StatusFailed {
		return s == StatusCreated || s == StatusProcessing || s == StatusTranscribed || s == StatusSummarized
	}

	switch s {
	case StatusCreated:
		return next == StatusProcessing
	case StatusProcessing:
		return next == StatusTranscribed
	case StatusTranscribed:
		return next == StatusSummarized
	case StatusSummarized:
		return next == StatusCompleted
	default:
		return false
	}
}

func (j ProcessingJob) Transition(next JobStatus, at time.Time, failure string) (ProcessingJob, error) {
	if !j.Status.CanTransitionTo(next) {
		return ProcessingJob{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, j.Status, next)
	}
	if next == StatusFailed && strings.TrimSpace(failure) == "" {
		return ProcessingJob{}, fmt.Errorf("%w: failure reason is required", ErrInvalidArgument)
	}
	if at.IsZero() {
		return ProcessingJob{}, fmt.Errorf("%w: transition time is required", ErrInvalidArgument)
	}

	j.Status = next
	j.UpdatedAt = at
	j.Error = ""
	if next == StatusProcessing {
		startedAt := at
		j.StartedAt = &startedAt
	}
	if next == StatusCompleted || next == StatusFailed {
		completedAt := at
		j.CompletedAt = &completedAt
	}
	if next == StatusFailed {
		j.Error = failure
	}
	return j, nil
}

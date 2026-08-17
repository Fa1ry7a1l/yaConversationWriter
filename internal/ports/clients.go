package ports

import (
	"context"

	"yaConversationWriter/internal/domain"
)

type AudioInput struct {
	MeetingID domain.MeetingID
	File      domain.FileMetadata
	Content   []byte
}

type SpeechClient interface {
	Transcribe(ctx context.Context, input AudioInput) (string, error)
}

type SummaryRequest struct {
	MeetingID  domain.MeetingID
	Transcript string
}

type AnswerRequest struct {
	MeetingID  domain.MeetingID
	Transcript string
	Summary    string
	Question   string
}

type LLMClient interface {
	Summarize(ctx context.Context, request SummaryRequest) (string, error)
	Answer(ctx context.Context, request AnswerRequest) (string, error)
}

type ProcessingTask struct {
	UserID    domain.UserID
	MeetingID domain.MeetingID
	Audio     AudioInput
}

// JobDispatcher is consumed by the upload use case and implemented by the
// bounded worker pool. It deliberately exposes no worker implementation details.
type JobDispatcher interface {
	Enqueue(ctx context.Context, task ProcessingTask) error
}

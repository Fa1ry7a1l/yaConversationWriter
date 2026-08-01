package ports

import (
	"context"

	"yaConversationWriter/internal/domain"
)

type UploadMeetingRequest struct {
	ExternalUserID string
	File           domain.FileMetadata
	Content        []byte
}

type UploadMeetingResult struct {
	Meeting domain.Meeting
	Job     domain.ProcessingJob
}

// MeetingApplication is the transport-facing application contract. Telegram
// handlers will depend on this interface rather than concrete services.
type MeetingApplication interface {
	RegisterUser(ctx context.Context, externalUserID string) (domain.User, error)
	UploadMeeting(ctx context.Context, request UploadMeetingRequest) (UploadMeetingResult, error)
	ListMeetings(ctx context.Context, externalUserID string) ([]domain.MeetingRecord, error)
	GetStatus(ctx context.Context, externalUserID string, meetingID domain.MeetingID) (domain.ProcessingJob, error)
	GetTranscript(ctx context.Context, externalUserID string, meetingID domain.MeetingID) (domain.Transcript, error)
	FindMeetings(ctx context.Context, externalUserID, keyword string) ([]domain.SearchResult, error)
	Chat(ctx context.Context, externalUserID string, meetingID domain.MeetingID, question string) (string, error)
}

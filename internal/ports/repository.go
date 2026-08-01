package ports

import (
	"context"

	"yaConversationWriter/internal/domain"
)

type UserRepository interface {
	GetOrCreateUser(ctx context.Context, externalID string) (domain.User, error)
	GetUserByExternalID(ctx context.Context, externalID string) (domain.User, error)
}

type MeetingRepository interface {
	CreateMeetingWithJob(ctx context.Context, userID domain.UserID, metadata domain.FileMetadata) (domain.Meeting, domain.ProcessingJob, error)
	GetMeeting(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Meeting, error)
	ListMeetings(ctx context.Context, userID domain.UserID) ([]domain.MeetingRecord, error)
	FindMeetings(ctx context.Context, userID domain.UserID, keyword string) ([]domain.SearchResult, error)
	GetTranscript(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Transcript, error)
	GetSummary(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Summary, error)
}

type ProcessingRepository interface {
	GetJob(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error)
	MarkProcessing(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error)
	SaveTranscript(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, text string) (domain.Transcript, domain.ProcessingJob, error)
	SaveSummary(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, text string) (domain.Summary, domain.ProcessingJob, error)
	MarkCompleted(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error)
	MarkFailed(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, reason string) (domain.ProcessingJob, error)
}

type Repository interface {
	UserRepository
	MeetingRepository
	ProcessingRepository
}

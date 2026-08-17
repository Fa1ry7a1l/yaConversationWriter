package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

var _ ports.MeetingApplication = (*Meetings)(nil)

type Meetings struct {
	repository ports.Repository
	llm        ports.LLMClient
	dispatcher ports.JobDispatcher
	logger     *slog.Logger
}

func NewMeetings(repository ports.Repository, llm ports.LLMClient, dispatcher ports.JobDispatcher, logger *slog.Logger) (*Meetings, error) {
	if repository == nil || llm == nil || dispatcher == nil {
		return nil, errors.New("repository, LLM client and job dispatcher are required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Meetings{repository: repository, llm: llm, dispatcher: dispatcher, logger: logger}, nil
}

func (s *Meetings) RegisterUser(ctx context.Context, externalUserID string) (domain.User, error) {
	user, err := s.repository.GetOrCreateUser(ctx, externalUserID)
	if err != nil {
		return domain.User{}, fmt.Errorf("register user: %w", err)
	}
	s.logger.Info("user registered", "user_id", user.ID)
	return user, nil
}

func (s *Meetings) UploadMeeting(ctx context.Context, request ports.UploadMeetingRequest) (ports.UploadMeetingResult, error) {
	if err := validateUpload(request); err != nil {
		return ports.UploadMeetingResult{}, err
	}

	user, err := s.repository.GetOrCreateUser(ctx, request.ExternalUserID)
	if err != nil {
		return ports.UploadMeetingResult{}, fmt.Errorf("resolve upload user: %w", err)
	}
	meeting, job, err := s.repository.CreateMeetingWithJob(ctx, user.ID, request.File)
	if err != nil {
		return ports.UploadMeetingResult{}, fmt.Errorf("create meeting and processing job: %w", err)
	}
	result := ports.UploadMeetingResult{Meeting: meeting, Job: job}
	s.logger.Info("meeting and processing job created", "user_id", user.ID, "meeting_id", meeting.ID, "job_id", job.ID)

	task := ports.ProcessingTask{
		UserID:    user.ID,
		MeetingID: meeting.ID,
		Audio: ports.AudioInput{
			MeetingID: meeting.ID,
			File:      request.File,
			Content:   append([]byte(nil), request.Content...),
		},
	}
	if err := s.dispatcher.Enqueue(ctx, task); err != nil {
		enqueueErr := fmt.Errorf("enqueue meeting %q: %w", meeting.ID, err)
		failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultFailureSaveTimeout)
		failedJob, saveErr := s.repository.MarkFailed(failureCtx, user.ID, meeting.ID, enqueueErr.Error())
		cancel()
		if saveErr == nil {
			result.Job = failedJob
		} else {
			enqueueErr = errors.Join(enqueueErr, fmt.Errorf("persist enqueue failure: %w", saveErr))
		}
		s.logger.Error("meeting enqueue failed", "user_id", user.ID, "meeting_id", meeting.ID, "error", enqueueErr)
		return result, enqueueErr
	}

	s.logger.Info("meeting queued", "user_id", user.ID, "meeting_id", meeting.ID)
	return result, nil
}

func (s *Meetings) ListMeetings(ctx context.Context, externalUserID string) ([]domain.MeetingRecord, error) {
	user, err := s.repository.GetUserByExternalID(ctx, externalUserID)
	if err != nil {
		return nil, fmt.Errorf("resolve list user: %w", err)
	}
	records, err := s.repository.ListMeetings(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list meetings: %w", err)
	}
	return records, nil
}

func (s *Meetings) GetStatus(ctx context.Context, externalUserID string, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	user, err := s.readUserAndValidateMeetingID(ctx, externalUserID, meetingID)
	if err != nil {
		return domain.ProcessingJob{}, err
	}
	job, err := s.repository.GetJob(ctx, user.ID, meetingID)
	if err != nil {
		return domain.ProcessingJob{}, fmt.Errorf("get meeting status: %w", err)
	}
	return job, nil
}

func (s *Meetings) GetTranscript(ctx context.Context, externalUserID string, meetingID domain.MeetingID) (domain.Transcript, error) {
	user, err := s.readUserAndValidateMeetingID(ctx, externalUserID, meetingID)
	if err != nil {
		return domain.Transcript{}, err
	}
	transcript, err := s.repository.GetTranscript(ctx, user.ID, meetingID)
	if err != nil {
		return domain.Transcript{}, fmt.Errorf("get meeting transcript: %w", err)
	}
	return transcript, nil
}

func (s *Meetings) FindMeetings(ctx context.Context, externalUserID, keyword string) ([]domain.SearchResult, error) {
	user, err := s.repository.GetUserByExternalID(ctx, externalUserID)
	if err != nil {
		return nil, fmt.Errorf("resolve search user: %w", err)
	}
	results, err := s.repository.FindMeetings(ctx, user.ID, keyword)
	if err != nil {
		return nil, fmt.Errorf("find meetings: %w", err)
	}
	return results, nil
}

func (s *Meetings) Chat(ctx context.Context, externalUserID string, meetingID domain.MeetingID, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("%w: question is required", domain.ErrInvalidArgument)
	}
	user, err := s.readUserAndValidateMeetingID(ctx, externalUserID, meetingID)
	if err != nil {
		return "", err
	}
	job, err := s.repository.GetJob(ctx, user.ID, meetingID)
	if err != nil {
		return "", fmt.Errorf("get meeting status for chat: %w", err)
	}
	if job.Status != domain.StatusCompleted {
		return "", fmt.Errorf("%w: current status is %s", domain.ErrNotReady, job.Status)
	}
	transcript, err := s.repository.GetTranscript(ctx, user.ID, meetingID)
	if err != nil {
		return "", fmt.Errorf("get chat transcript: %w", err)
	}
	summary, err := s.repository.GetSummary(ctx, user.ID, meetingID)
	if err != nil {
		return "", fmt.Errorf("get chat summary: %w", err)
	}

	s.logger.Info("calling LLM answer", "user_id", user.ID, "meeting_id", meetingID)
	answer, err := s.llm.Answer(ctx, ports.AnswerRequest{
		MeetingID:  meetingID,
		Transcript: transcript.Text,
		Summary:    summary.Text,
		Question:   question,
	})
	if err != nil {
		return "", fmt.Errorf("answer meeting question: %w", err)
	}
	return answer, nil
}

func (s *Meetings) readUserAndValidateMeetingID(ctx context.Context, externalUserID string, meetingID domain.MeetingID) (domain.User, error) {
	if meetingID == "" {
		return domain.User{}, fmt.Errorf("%w: meeting ID is required", domain.ErrInvalidArgument)
	}
	user, err := s.repository.GetUserByExternalID(ctx, externalUserID)
	if err != nil {
		return domain.User{}, fmt.Errorf("resolve meeting user: %w", err)
	}
	return user, nil
}

func validateUpload(request ports.UploadMeetingRequest) error {
	if strings.TrimSpace(request.ExternalUserID) == "" {
		return fmt.Errorf("%w: external user ID is required", domain.ErrInvalidArgument)
	}
	if strings.TrimSpace(request.File.ExternalFileID) == "" {
		return fmt.Errorf("%w: external file ID is required", domain.ErrInvalidArgument)
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(request.File.MIMEType)), "audio/") {
		return fmt.Errorf("%w: MIME type %q", domain.ErrUnsupportedFile, request.File.MIMEType)
	}
	if len(request.Content) == 0 {
		return fmt.Errorf("%w: audio content is empty", domain.ErrInvalidArgument)
	}
	if request.File.SizeBytes < 0 || request.File.Duration < 0 {
		return fmt.Errorf("%w: file size and duration must not be negative", domain.ErrInvalidArgument)
	}
	return nil
}

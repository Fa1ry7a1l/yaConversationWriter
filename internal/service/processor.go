package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

const defaultFailureSaveTimeout = 5 * time.Second

type Processor struct {
	repository         ports.ProcessingRepository
	speech             ports.SpeechClient
	llm                ports.LLMClient
	logger             *slog.Logger
	failureSaveTimeout time.Duration
}

func NewProcessor(repository ports.ProcessingRepository, speech ports.SpeechClient, llm ports.LLMClient, logger *slog.Logger) (*Processor, error) {
	if repository == nil || speech == nil || llm == nil {
		return nil, errors.New("processing repository, speech client and LLM client are required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Processor{
		repository:         repository,
		speech:             speech,
		llm:                llm,
		logger:             logger,
		failureSaveTimeout: defaultFailureSaveTimeout,
	}, nil
}

func (p *Processor) Process(ctx context.Context, task ports.ProcessingTask) error {
	if task.UserID == "" || task.MeetingID == "" {
		return fmt.Errorf("%w: processing task user and meeting IDs are required", domain.ErrInvalidArgument)
	}
	if task.Audio.MeetingID != task.MeetingID {
		return p.fail(ctx, task, fmt.Errorf("%w: audio meeting ID does not match task", domain.ErrInvalidArgument))
	}
	if len(task.Audio.Content) == 0 {
		return p.fail(ctx, task, fmt.Errorf("%w: audio content is empty", domain.ErrInvalidArgument))
	}

	p.logger.Info("meeting processing started", "user_id", task.UserID, "meeting_id", task.MeetingID)
	if _, err := p.repository.MarkProcessing(ctx, task.UserID, task.MeetingID); err != nil {
		return p.fail(ctx, task, fmt.Errorf("mark processing: %w", err))
	}

	p.logger.Info("calling speech client", "user_id", task.UserID, "meeting_id", task.MeetingID)
	transcript, err := p.speech.Transcribe(ctx, task.Audio)
	if err != nil {
		return p.fail(ctx, task, fmt.Errorf("transcribe audio: %w", err))
	}
	if _, _, err := p.repository.SaveTranscript(ctx, task.UserID, task.MeetingID, transcript); err != nil {
		return p.fail(ctx, task, fmt.Errorf("save transcript: %w", err))
	}

	p.logger.Info("calling LLM summary", "user_id", task.UserID, "meeting_id", task.MeetingID)
	summary, err := p.llm.Summarize(ctx, ports.SummaryRequest{
		MeetingID:  task.MeetingID,
		Transcript: transcript,
	})
	if err != nil {
		return p.fail(ctx, task, fmt.Errorf("summarize transcript: %w", err))
	}
	if _, _, err := p.repository.SaveSummary(ctx, task.UserID, task.MeetingID, summary); err != nil {
		return p.fail(ctx, task, fmt.Errorf("save summary: %w", err))
	}
	if _, err := p.repository.MarkCompleted(ctx, task.UserID, task.MeetingID); err != nil {
		return p.fail(ctx, task, fmt.Errorf("mark completed: %w", err))
	}

	p.logger.Info("meeting processing completed", "user_id", task.UserID, "meeting_id", task.MeetingID)
	return nil
}

func (p *Processor) fail(ctx context.Context, task ports.ProcessingTask, processingErr error) error {
	reason := strings.TrimSpace(processingErr.Error())
	const maxReasonRunes = 2000
	if runes := []rune(reason); len(runes) > maxReasonRunes {
		reason = string(runes[:maxReasonRunes])
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.failureSaveTimeout)
	defer cancel()
	_, saveErr := p.repository.MarkFailed(saveCtx, task.UserID, task.MeetingID, reason)
	if saveErr != nil {
		p.logger.Error("failed to persist processing error", "user_id", task.UserID, "meeting_id", task.MeetingID, "error", saveErr)
		return errors.Join(processingErr, fmt.Errorf("persist failed status: %w", saveErr))
	}
	p.logger.Error("meeting processing failed", "user_id", task.UserID, "meeting_id", task.MeetingID, "error", processingErr)
	return processingErr
}

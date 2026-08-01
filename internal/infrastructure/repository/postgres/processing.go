package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"yaConversationWriter/internal/domain"
)

func (r *Repository) GetJob(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	job, err := scanJob(r.pool.QueryRow(ctx, `
        SELECT `+jobColumns+`
        FROM processing_jobs j
        JOIN meetings m ON m.id = j.meeting_id
        WHERE m.user_id = $1::uuid AND m.id = $2::uuid
    `, userID, meetingID))
	if err != nil {
		return domain.ProcessingJob{}, r.mapError("get owned processing job", err)
	}
	return job, nil
}

func (r *Repository) MarkProcessing(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	return r.transition(ctx, userID, meetingID, domain.StatusProcessing, "", []domain.JobStatus{domain.StatusCreated})
}

func (r *Repository) SaveTranscript(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, text string) (domain.Transcript, domain.ProcessingJob, error) {
	if strings.TrimSpace(text) == "" {
		return domain.Transcript{}, domain.ProcessingJob{}, fmt.Errorf("%w: transcript text is required", domain.ErrInvalidArgument)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, r.mapError("begin transcript transaction", err)
	}
	defer rollback(tx)

	job, err := r.lockOwnedJob(ctx, tx, userID, meetingID)
	if err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, err
	}
	if job.Status != domain.StatusProcessing {
		return domain.Transcript{}, domain.ProcessingJob{}, fmt.Errorf("%w: %s -> %s", domain.ErrInvalidTransition, job.Status, domain.StatusTranscribed)
	}

	var transcript domain.Transcript
	var transcriptMeetingID string
	err = tx.QueryRow(ctx, `
        INSERT INTO transcripts (meeting_id, text)
        VALUES ($1::uuid, $2)
        RETURNING meeting_id::text, text, created_at, updated_at
    `, meetingID, text).Scan(&transcriptMeetingID, &transcript.Text, &transcript.CreatedAt, &transcript.UpdatedAt)
	if err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, r.mapError("insert transcript", err)
	}
	transcript.MeetingID = domain.MeetingID(transcriptMeetingID)
	job, err = scanJob(tx.QueryRow(ctx, `
        UPDATE processing_jobs
        SET status = 'transcribed', error = '', updated_at = now()
        WHERE meeting_id = $1::uuid AND status = 'processing'
        RETURNING id::text, meeting_id::text, status, error, created_at, updated_at, started_at, completed_at
    `, meetingID))
	if err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, r.mapError("mark job transcribed", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, r.mapError("commit transcript transaction", err)
	}
	return transcript, job, nil
}

func (r *Repository) SaveSummary(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, text string) (domain.Summary, domain.ProcessingJob, error) {
	if strings.TrimSpace(text) == "" {
		return domain.Summary{}, domain.ProcessingJob{}, fmt.Errorf("%w: summary text is required", domain.ErrInvalidArgument)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, r.mapError("begin summary transaction", err)
	}
	defer rollback(tx)

	job, err := r.lockOwnedJob(ctx, tx, userID, meetingID)
	if err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, err
	}
	if job.Status != domain.StatusTranscribed {
		return domain.Summary{}, domain.ProcessingJob{}, fmt.Errorf("%w: %s -> %s", domain.ErrInvalidTransition, job.Status, domain.StatusSummarized)
	}

	var summary domain.Summary
	var summaryMeetingID string
	err = tx.QueryRow(ctx, `
        INSERT INTO summaries (meeting_id, text)
        VALUES ($1::uuid, $2)
        RETURNING meeting_id::text, text, created_at, updated_at
    `, meetingID, text).Scan(&summaryMeetingID, &summary.Text, &summary.CreatedAt, &summary.UpdatedAt)
	if err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, r.mapError("insert summary", err)
	}
	summary.MeetingID = domain.MeetingID(summaryMeetingID)
	job, err = scanJob(tx.QueryRow(ctx, `
        UPDATE processing_jobs
        SET status = 'summarized', error = '', updated_at = now()
        WHERE meeting_id = $1::uuid AND status = 'transcribed'
        RETURNING id::text, meeting_id::text, status, error, created_at, updated_at, started_at, completed_at
    `, meetingID))
	if err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, r.mapError("mark job summarized", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, r.mapError("commit summary transaction", err)
	}
	return summary, job, nil
}

func (r *Repository) MarkCompleted(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	return r.transition(ctx, userID, meetingID, domain.StatusCompleted, "", []domain.JobStatus{domain.StatusSummarized})
}

func (r *Repository) MarkFailed(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, reason string) (domain.ProcessingJob, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return domain.ProcessingJob{}, fmt.Errorf("%w: failure reason is required", domain.ErrInvalidArgument)
	}
	return r.transition(ctx, userID, meetingID, domain.StatusFailed, reason, []domain.JobStatus{
		domain.StatusCreated,
		domain.StatusProcessing,
		domain.StatusTranscribed,
		domain.StatusSummarized,
	})
}

func (r *Repository) transition(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, next domain.JobStatus, reason string, allowed []domain.JobStatus) (domain.ProcessingJob, error) {
	allowedStatuses := make([]string, len(allowed))
	for i, status := range allowed {
		allowedStatuses[i] = string(status)
	}
	job, err := scanJob(r.pool.QueryRow(ctx, `
        UPDATE processing_jobs j
        SET status = $3,
            error = CASE WHEN $3 = 'failed' THEN $4 ELSE '' END,
            updated_at = now(),
            started_at = CASE WHEN $3 = 'processing' THEN now() ELSE j.started_at END,
            completed_at = CASE WHEN $3 IN ('completed', 'failed') THEN now() ELSE j.completed_at END
        FROM meetings m
        WHERE m.id = j.meeting_id
          AND m.user_id = $1::uuid
          AND m.id = $2::uuid
          AND j.status = ANY($5::text[])
        RETURNING j.id::text, j.meeting_id::text, j.status, j.error,
                  j.created_at, j.updated_at, j.started_at, j.completed_at
    `, userID, meetingID, next, reason, allowedStatuses))
	if err == nil {
		return job, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.ProcessingJob{}, r.mapError("transition processing job", err)
	}
	existing, getErr := r.GetJob(ctx, userID, meetingID)
	if getErr != nil {
		return domain.ProcessingJob{}, getErr
	}
	return domain.ProcessingJob{}, fmt.Errorf("%w: %s -> %s", domain.ErrInvalidTransition, existing.Status, next)
}

func (r *Repository) lockOwnedJob(ctx context.Context, tx pgx.Tx, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	job, err := scanJob(tx.QueryRow(ctx, `
        SELECT `+jobColumns+`
        FROM processing_jobs j
        JOIN meetings m ON m.id = j.meeting_id
        WHERE m.user_id = $1::uuid AND m.id = $2::uuid
        FOR UPDATE OF j
    `, userID, meetingID))
	if err != nil {
		return domain.ProcessingJob{}, r.mapError("lock owned processing job", err)
	}
	return job, nil
}

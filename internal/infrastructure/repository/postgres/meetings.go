package postgres

import (
	"context"
	"fmt"
	"strings"

	"yaConversationWriter/internal/domain"
)

func (r *Repository) CreateMeetingWithJob(ctx context.Context, userID domain.UserID, metadata domain.FileMetadata) (domain.Meeting, domain.ProcessingJob, error) {
	if userID == "" || strings.TrimSpace(metadata.ExternalFileID) == "" {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("%w: user ID and external file ID are required", domain.ErrInvalidArgument)
	}
	if metadata.SizeBytes < 0 || metadata.Duration < 0 {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("%w: file size and duration must not be negative", domain.ErrInvalidArgument)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, r.mapError("begin meeting transaction", err)
	}
	defer rollback(tx)

	meeting, err := scanMeeting(tx.QueryRow(ctx, `
        INSERT INTO meetings (user_id, external_file_id, file_name, mime_type, size_bytes, duration_ms)
        VALUES ($1::uuid, $2, $3, $4, $5, $6)
        RETURNING id::text, user_id::text, external_file_id, file_name, mime_type,
                  size_bytes, duration_ms, created_at, updated_at
    `, userID, metadata.ExternalFileID, metadata.FileName, metadata.MIMEType, metadata.SizeBytes, metadata.Duration.Milliseconds()))
	if err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, r.mapError("insert meeting", err)
	}
	job, err := scanJob(tx.QueryRow(ctx, `
        INSERT INTO processing_jobs (meeting_id, status)
        VALUES ($1::uuid, 'created')
        RETURNING id::text, meeting_id::text, status, error, created_at, updated_at, started_at, completed_at
    `, meeting.ID))
	if err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, r.mapError("insert processing job", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, r.mapError("commit meeting transaction", err)
	}
	return meeting, job, nil
}

func (r *Repository) GetMeeting(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Meeting, error) {
	meeting, err := scanMeeting(r.pool.QueryRow(ctx, `
        SELECT `+meetingColumns+`
        FROM meetings m
        WHERE m.user_id = $1::uuid AND m.id = $2::uuid
    `, userID, meetingID))
	if err != nil {
		return domain.Meeting{}, r.mapError("get owned meeting", err)
	}
	return meeting, nil
}

func (r *Repository) ListMeetings(ctx context.Context, userID domain.UserID) ([]domain.MeetingRecord, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT `+meetingRecordColumns+`
        FROM meetings m
        JOIN processing_jobs j ON j.meeting_id = m.id
        LEFT JOIN summaries s ON s.meeting_id = m.id
        WHERE m.user_id = $1::uuid
        ORDER BY m.created_at DESC, m.id DESC
    `, userID)
	if err != nil {
		return nil, r.mapError("list owned meetings", err)
	}
	defer rows.Close()

	records := make([]domain.MeetingRecord, 0)
	for rows.Next() {
		record, scanErr := scanMeetingRecord(rows)
		if scanErr != nil {
			return nil, r.mapError("scan meeting list", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, r.mapError("read meeting list", err)
	}
	return records, nil
}

func (r *Repository) FindMeetings(ctx context.Context, userID domain.UserID, keyword string) ([]domain.SearchResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("%w: search keyword is required", domain.ErrInvalidArgument)
	}
	pattern := "%" + escapeLike(keyword) + "%"
	rows, err := r.pool.Query(ctx, `
        SELECT `+meetingRecordColumns+`,
               CASE
                   WHEN t.text ILIKE $2 ESCAPE '\' THEN t.text
                   WHEN s.text ILIKE $2 ESCAPE '\' THEN s.text
                   ELSE m.file_name
               END AS matched_text
        FROM meetings m
        JOIN processing_jobs j ON j.meeting_id = m.id
        LEFT JOIN transcripts t ON t.meeting_id = m.id
        LEFT JOIN summaries s ON s.meeting_id = m.id
        WHERE m.user_id = $1::uuid
          AND (
              t.text ILIKE $2 ESCAPE '\'
              OR s.text ILIKE $2 ESCAPE '\'
              OR m.file_name ILIKE $2 ESCAPE '\'
          )
        ORDER BY m.created_at DESC, m.id DESC
    `, userID, pattern)
	if err != nil {
		return nil, r.mapError("find owned meetings", err)
	}
	defer rows.Close()

	results := make([]domain.SearchResult, 0)
	for rows.Next() {
		record, source, scanErr := scanMeetingRecordWithSource(rows, true)
		if scanErr != nil {
			return nil, r.mapError("scan meeting search", scanErr)
		}
		results = append(results, domain.SearchResult{Record: record, Snippet: textSnippet(source)})
	}
	if err := rows.Err(); err != nil {
		return nil, r.mapError("read meeting search", err)
	}
	return results, nil
}

func (r *Repository) GetTranscript(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Transcript, error) {
	var transcript domain.Transcript
	var id string
	err := r.pool.QueryRow(ctx, `
        SELECT t.meeting_id::text, t.text, t.created_at, t.updated_at
        FROM transcripts t
        JOIN meetings m ON m.id = t.meeting_id
        WHERE m.user_id = $1::uuid AND m.id = $2::uuid
    `, userID, meetingID).Scan(&id, &transcript.Text, &transcript.CreatedAt, &transcript.UpdatedAt)
	if err != nil {
		return domain.Transcript{}, r.mapError("get owned transcript", err)
	}
	transcript.MeetingID = domain.MeetingID(id)
	return transcript, nil
}

func (r *Repository) GetSummary(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Summary, error) {
	var summary domain.Summary
	var id string
	err := r.pool.QueryRow(ctx, `
        SELECT s.meeting_id::text, s.text, s.created_at, s.updated_at
        FROM summaries s
        JOIN meetings m ON m.id = s.meeting_id
        WHERE m.user_id = $1::uuid AND m.id = $2::uuid
    `, userID, meetingID).Scan(&id, &summary.Text, &summary.CreatedAt, &summary.UpdatedAt)
	if err != nil {
		return domain.Summary{}, r.mapError("get owned summary", err)
	}
	summary.MeetingID = domain.MeetingID(id)
	return summary, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func textSnippet(value string) string {
	const maximumRunes = 160
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximumRunes {
		return string(runes)
	}
	return string(runes[:maximumRunes]) + "…"
}

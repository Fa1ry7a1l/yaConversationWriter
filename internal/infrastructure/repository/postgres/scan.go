package postgres

import (
	"database/sql"
	"time"

	"yaConversationWriter/internal/domain"
)

func scanMeeting(row scanner) (domain.Meeting, error) {
	var meeting domain.Meeting
	var meetingID, userID string
	var durationMilliseconds int64
	if err := row.Scan(
		&meetingID,
		&userID,
		&meeting.File.ExternalFileID,
		&meeting.File.FileName,
		&meeting.File.MIMEType,
		&meeting.File.SizeBytes,
		&durationMilliseconds,
		&meeting.CreatedAt,
		&meeting.UpdatedAt,
	); err != nil {
		return domain.Meeting{}, err
	}
	meeting.ID = domain.MeetingID(meetingID)
	meeting.UserID = domain.UserID(userID)
	meeting.File.Duration = time.Duration(durationMilliseconds) * time.Millisecond
	return meeting, nil
}

func scanJob(row scanner) (domain.ProcessingJob, error) {
	var job domain.ProcessingJob
	var jobID, meetingID, status string
	if err := row.Scan(
		&jobID,
		&meetingID,
		&status,
		&job.Error,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.CompletedAt,
	); err != nil {
		return domain.ProcessingJob{}, err
	}
	job.ID = domain.JobID(jobID)
	job.MeetingID = domain.MeetingID(meetingID)
	job.Status = domain.JobStatus(status)
	return job, nil
}

func scanMeetingRecord(row scanner) (domain.MeetingRecord, error) {
	record, _, err := scanMeetingRecordWithSource(row, false)
	return record, err
}

func scanMeetingRecordWithSource(row scanner, includeSource bool) (domain.MeetingRecord, string, error) {
	var record domain.MeetingRecord
	var meetingID, userID, jobID, jobMeetingID, status string
	var durationMilliseconds int64
	var summaryText sql.NullString
	var summaryCreatedAt, summaryUpdatedAt sql.NullTime
	var source string
	destinations := []any{
		&meetingID,
		&userID,
		&record.Meeting.File.ExternalFileID,
		&record.Meeting.File.FileName,
		&record.Meeting.File.MIMEType,
		&record.Meeting.File.SizeBytes,
		&durationMilliseconds,
		&record.Meeting.CreatedAt,
		&record.Meeting.UpdatedAt,
		&jobID,
		&jobMeetingID,
		&status,
		&record.Job.Error,
		&record.Job.CreatedAt,
		&record.Job.UpdatedAt,
		&record.Job.StartedAt,
		&record.Job.CompletedAt,
		&summaryText,
		&summaryCreatedAt,
		&summaryUpdatedAt,
	}
	if includeSource {
		destinations = append(destinations, &source)
	}
	if err := row.Scan(destinations...); err != nil {
		return domain.MeetingRecord{}, "", err
	}
	record.Meeting.ID = domain.MeetingID(meetingID)
	record.Meeting.UserID = domain.UserID(userID)
	record.Meeting.File.Duration = time.Duration(durationMilliseconds) * time.Millisecond
	record.Job.ID = domain.JobID(jobID)
	record.Job.MeetingID = domain.MeetingID(jobMeetingID)
	record.Job.Status = domain.JobStatus(status)
	if summaryText.Valid {
		record.Summary = &domain.Summary{
			MeetingID: record.Meeting.ID,
			Text:      summaryText.String,
			CreatedAt: summaryCreatedAt.Time,
			UpdatedAt: summaryUpdatedAt.Time,
		}
	}
	return record, source, nil
}

const meetingColumns = `
    m.id::text,
    m.user_id::text,
    m.external_file_id,
    m.file_name,
    m.mime_type,
    m.size_bytes,
    m.duration_ms,
    m.created_at,
    m.updated_at
`

const jobColumns = `
    j.id::text,
    j.meeting_id::text,
    j.status,
    j.error,
    j.created_at,
    j.updated_at,
    j.started_at,
    j.completed_at
`

const meetingRecordColumns = meetingColumns + `,
` + jobColumns + `,
    s.text,
    s.created_at,
    s.updated_at
`

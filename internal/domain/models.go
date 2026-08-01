package domain

import "time"

type UserID string
type MeetingID string
type JobID string

type User struct {
	ID         UserID
	ExternalID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type FileMetadata struct {
	ExternalFileID string
	FileName       string
	MIMEType       string
	SizeBytes      int64
	Duration       time.Duration
}

type Meeting struct {
	ID        MeetingID
	UserID    UserID
	File      FileMetadata
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ProcessingJob struct {
	ID          JobID
	MeetingID   MeetingID
	Status      JobStatus
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type Transcript struct {
	MeetingID MeetingID
	Text      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Summary struct {
	MeetingID MeetingID
	Text      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type MeetingRecord struct {
	Meeting Meeting
	Job     ProcessingJob
	Summary *Summary
}

type SearchResult struct {
	Record  MeetingRecord
	Snippet string
}

package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"yaConversationWriter/internal/domain"
	"yaConversationWriter/internal/ports"
)

var _ ports.Repository = (*Repository)(nil)

type Repository struct {
	mu sync.RWMutex

	now func() time.Time

	usersByID      map[domain.UserID]domain.User
	userByExternal map[string]domain.UserID
	meetings       map[domain.MeetingID]domain.Meeting
	jobs           map[domain.MeetingID]domain.ProcessingJob
	transcripts    map[domain.MeetingID]domain.Transcript
	summaries      map[domain.MeetingID]domain.Summary
}

func New() *Repository {
	return NewWithClock(time.Now)
}

func NewWithClock(now func() time.Time) *Repository {
	if now == nil {
		now = time.Now
	}
	return &Repository{
		now:            now,
		usersByID:      make(map[domain.UserID]domain.User),
		userByExternal: make(map[string]domain.UserID),
		meetings:       make(map[domain.MeetingID]domain.Meeting),
		jobs:           make(map[domain.MeetingID]domain.ProcessingJob),
		transcripts:    make(map[domain.MeetingID]domain.Transcript),
		summaries:      make(map[domain.MeetingID]domain.Summary),
	}
}

func (r *Repository) GetOrCreateUser(ctx context.Context, externalID string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return domain.User{}, fmt.Errorf("%w: external user ID is required", domain.ErrInvalidArgument)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if id, ok := r.userByExternal[externalID]; ok {
		return r.usersByID[id], nil
	}

	id, err := newUserID()
	if err != nil {
		return domain.User{}, fmt.Errorf("generate user ID: %w", err)
	}
	now := r.timestamp()
	user := domain.User{ID: id, ExternalID: externalID, CreatedAt: now, UpdatedAt: now}
	r.usersByID[id] = user
	r.userByExternal[externalID] = id
	return user, nil
}

func (r *Repository) GetUserByExternalID(ctx context.Context, externalID string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return domain.User{}, fmt.Errorf("%w: external user ID is required", domain.ErrInvalidArgument)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.userByExternal[externalID]
	if !ok {
		return domain.User{}, fmt.Errorf("external user %q: %w", externalID, domain.ErrNotFound)
	}
	return r.usersByID[id], nil
}

func (r *Repository) CreateMeetingWithJob(ctx context.Context, userID domain.UserID, metadata domain.FileMetadata) (domain.Meeting, domain.ProcessingJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, err
	}
	if userID == "" || strings.TrimSpace(metadata.ExternalFileID) == "" {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("%w: user ID and external file ID are required", domain.ErrInvalidArgument)
	}
	if metadata.SizeBytes < 0 || metadata.Duration < 0 {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("%w: file size and duration must not be negative", domain.ErrInvalidArgument)
	}

	meetingID, err := newMeetingID()
	if err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("generate meeting ID: %w", err)
	}
	jobID, err := newJobID()
	if err != nil {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("generate job ID: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.usersByID[userID]; !ok {
		return domain.Meeting{}, domain.ProcessingJob{}, fmt.Errorf("user %q: %w", userID, domain.ErrNotFound)
	}

	now := r.timestamp()
	meeting := domain.Meeting{ID: meetingID, UserID: userID, File: metadata, CreatedAt: now, UpdatedAt: now}
	job := domain.ProcessingJob{ID: jobID, MeetingID: meetingID, Status: domain.StatusCreated, CreatedAt: now, UpdatedAt: now}

	// Both values become visible while holding the same lock, mirroring the
	// transaction required from the future PostgreSQL repository.
	r.meetings[meetingID] = meeting
	r.jobs[meetingID] = job
	return meeting, job, nil
}

func (r *Repository) GetMeeting(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Meeting, error) {
	if err := ctx.Err(); err != nil {
		return domain.Meeting{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ownedMeetingLocked(userID, meetingID)
}

func (r *Repository) GetJob(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProcessingJob{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, err := r.ownedMeetingLocked(userID, meetingID); err != nil {
		return domain.ProcessingJob{}, err
	}
	job, ok := r.jobs[meetingID]
	if !ok {
		return domain.ProcessingJob{}, fmt.Errorf("job for meeting %q: %w", meetingID, domain.ErrNotFound)
	}
	return cloneJob(job)
}

func (r *Repository) GetTranscript(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Transcript, error) {
	if err := ctx.Err(); err != nil {
		return domain.Transcript{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, err := r.ownedMeetingLocked(userID, meetingID); err != nil {
		return domain.Transcript{}, err
	}
	transcript, ok := r.transcripts[meetingID]
	if !ok {
		return domain.Transcript{}, fmt.Errorf("transcript for meeting %q: %w", meetingID, domain.ErrNotFound)
	}
	return transcript, nil
}

func (r *Repository) GetSummary(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.Summary, error) {
	if err := ctx.Err(); err != nil {
		return domain.Summary{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, err := r.ownedMeetingLocked(userID, meetingID); err != nil {
		return domain.Summary{}, err
	}
	summary, ok := r.summaries[meetingID]
	if !ok {
		return domain.Summary{}, fmt.Errorf("summary for meeting %q: %w", meetingID, domain.ErrNotFound)
	}
	return summary, nil
}

func (r *Repository) ListMeetings(ctx context.Context, userID domain.UserID) ([]domain.MeetingRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	records := make([]domain.MeetingRecord, 0)
	for id, meeting := range r.meetings {
		if meeting.UserID == userID {
			record, err := r.recordLocked(id)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	}
	sortRecords(records)
	return records, nil
}

func (r *Repository) FindMeetings(ctx context.Context, userID domain.UserID, keyword string) ([]domain.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("%w: search keyword is required", domain.ErrInvalidArgument)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make([]domain.SearchResult, 0)
	for id, meeting := range r.meetings {
		if meeting.UserID != userID {
			continue
		}
		candidate := meeting.File.FileName
		if transcript, ok := r.transcripts[id]; ok && containsFold(transcript.Text, keyword) {
			candidate = transcript.Text
		} else if summary, ok := r.summaries[id]; ok && containsFold(summary.Text, keyword) {
			candidate = summary.Text
		} else if !containsFold(candidate, keyword) {
			continue
		}
		record, err := r.recordLocked(id)
		if err != nil {
			return nil, err
		}
		results = append(results, domain.SearchResult{Record: record, Snippet: snippet(candidate)})
	}
	sort.Slice(results, func(i, j int) bool {
		return recordLess(results[i].Record, results[j].Record)
	})
	return results, nil
}

func (r *Repository) MarkProcessing(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	return r.transition(ctx, userID, meetingID, domain.StatusProcessing, "")
}

func (r *Repository) SaveTranscript(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, text string) (domain.Transcript, domain.ProcessingJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, err
	}
	if strings.TrimSpace(text) == "" {
		return domain.Transcript{}, domain.ProcessingJob{}, fmt.Errorf("%w: transcript text is required", domain.ErrInvalidArgument)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.ownedMeetingLocked(userID, meetingID); err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, err
	}
	now := r.timestamp()
	job, err := r.jobs[meetingID].Transition(domain.StatusTranscribed, now, "")
	if err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, err
	}
	jobCopy, err := cloneJob(job)
	if err != nil {
		return domain.Transcript{}, domain.ProcessingJob{}, err
	}
	transcript := domain.Transcript{MeetingID: meetingID, Text: text, CreatedAt: now, UpdatedAt: now}
	r.transcripts[meetingID] = transcript
	r.jobs[meetingID] = job
	return transcript, jobCopy, nil
}

func (r *Repository) SaveSummary(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, text string) (domain.Summary, domain.ProcessingJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, err
	}
	if strings.TrimSpace(text) == "" {
		return domain.Summary{}, domain.ProcessingJob{}, fmt.Errorf("%w: summary text is required", domain.ErrInvalidArgument)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.ownedMeetingLocked(userID, meetingID); err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, err
	}
	now := r.timestamp()
	job, err := r.jobs[meetingID].Transition(domain.StatusSummarized, now, "")
	if err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, err
	}
	jobCopy, err := cloneJob(job)
	if err != nil {
		return domain.Summary{}, domain.ProcessingJob{}, err
	}
	summary := domain.Summary{MeetingID: meetingID, Text: text, CreatedAt: now, UpdatedAt: now}
	r.summaries[meetingID] = summary
	r.jobs[meetingID] = job
	return summary, jobCopy, nil
}

func (r *Repository) MarkCompleted(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID) (domain.ProcessingJob, error) {
	return r.transition(ctx, userID, meetingID, domain.StatusCompleted, "")
}

func (r *Repository) MarkFailed(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, reason string) (domain.ProcessingJob, error) {
	return r.transition(ctx, userID, meetingID, domain.StatusFailed, reason)
}

func (r *Repository) transition(ctx context.Context, userID domain.UserID, meetingID domain.MeetingID, next domain.JobStatus, reason string) (domain.ProcessingJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProcessingJob{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.ownedMeetingLocked(userID, meetingID); err != nil {
		return domain.ProcessingJob{}, err
	}
	job, ok := r.jobs[meetingID]
	if !ok {
		return domain.ProcessingJob{}, fmt.Errorf("job for meeting %q: %w", meetingID, domain.ErrNotFound)
	}
	job, err := job.Transition(next, r.timestamp(), reason)
	if err != nil {
		return domain.ProcessingJob{}, err
	}
	jobCopy, err := cloneJob(job)
	if err != nil {
		return domain.ProcessingJob{}, err
	}
	r.jobs[meetingID] = job
	return jobCopy, nil
}

func (r *Repository) ownedMeetingLocked(userID domain.UserID, meetingID domain.MeetingID) (domain.Meeting, error) {
	meeting, ok := r.meetings[meetingID]
	if !ok || meeting.UserID != userID {
		// Deliberately hide whether a meeting owned by another user exists.
		return domain.Meeting{}, fmt.Errorf("meeting %q: %w", meetingID, domain.ErrNotFound)
	}
	return meeting, nil
}

func (r *Repository) recordLocked(meetingID domain.MeetingID) (domain.MeetingRecord, error) {
	job, err := cloneJob(r.jobs[meetingID])
	if err != nil {
		return domain.MeetingRecord{}, err
	}
	record := domain.MeetingRecord{Meeting: r.meetings[meetingID], Job: job}
	if summary, ok := r.summaries[meetingID]; ok {
		summaryCopy := summary
		record.Summary = &summaryCopy
	}
	return record, nil
}

func (r *Repository) timestamp() time.Time {
	return r.now().UTC()
}

func newUserID() (domain.UserID, error) {
	id, err := newID()
	return domain.UserID(id), err
}

func newMeetingID() (domain.MeetingID, error) {
	id, err := newID()
	return domain.MeetingID(id), err
}

func newJobID() (domain.JobID, error) {
	id, err := newID()
	return domain.JobID(id), err
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func cloneJob(job domain.ProcessingJob) (domain.ProcessingJob, error) {
	clone, err := clone(job)
	if err != nil {
		return domain.ProcessingJob{}, fmt.Errorf("clone processing job: %w", err)
	}
	return clone, nil
}

func clone[T any](value T) (T, error) {
	serialized, err := json.Marshal(value)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("serialize value: %w", err)
	}

	var result T
	if err := json.Unmarshal(serialized, &result); err != nil {
		var zero T
		return zero, fmt.Errorf("deserialize value: %w", err)
	}
	return result, nil
}

func sortRecords(records []domain.MeetingRecord) {
	sort.Slice(records, func(i, j int) bool { return recordLess(records[i], records[j]) })
}

func recordLess(left, right domain.MeetingRecord) bool {
	if left.Meeting.CreatedAt.Equal(right.Meeting.CreatedAt) {
		return left.Meeting.ID > right.Meeting.ID
	}
	return left.Meeting.CreatedAt.After(right.Meeting.CreatedAt)
}

func containsFold(text, keyword string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}

func snippet(text string) string {
	const maxRunes = 160
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "…"
}

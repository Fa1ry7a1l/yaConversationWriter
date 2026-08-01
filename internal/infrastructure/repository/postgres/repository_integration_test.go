//go:build integration

package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yaConversationWriter/internal/domain"
)

func TestMigrationsUpAndDown(t *testing.T) {
	repository := integrationRepository(t)
	ctx := context.Background()
	if err := repository.MigrateDown(ctx); err != nil {
		t.Fatalf("MigrateDown() error = %v", err)
	}
	var usersTable *string
	if err := repository.pool.QueryRow(ctx, "SELECT to_regclass('public.users')::text").Scan(&usersTable); err != nil {
		t.Fatalf("check users table after down: %v", err)
	}
	if usersTable != nil {
		t.Fatalf("users table still exists after down: %q", *usersTable)
	}
	if err := repository.MigrateUp(ctx); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}
	for _, relation := range []string{
		"users",
		"meetings",
		"processing_jobs",
		"transcripts",
		"summaries",
		"idx_meetings_user_created",
		"idx_meetings_user_id",
		"idx_processing_jobs_status_created",
		"idx_transcripts_text_trgm",
	} {
		var value *string
		if err := repository.pool.QueryRow(ctx, "SELECT to_regclass($1)::text", "public."+relation).Scan(&value); err != nil {
			t.Fatalf("check relation %s: %v", relation, err)
		}
		if value == nil {
			t.Fatalf("relation %s was not created", relation)
		}
	}
	for _, constraint := range []string{
		"users_external_id_key",
		"meetings_user_id_fkey",
		"processing_jobs_meeting_id_fkey",
		"processing_jobs_status_valid",
		"processing_jobs_failed_has_error",
	} {
		var exists bool
		if err := repository.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)", constraint).Scan(&exists); err != nil {
			t.Fatalf("check constraint %s: %v", constraint, err)
		}
		if !exists {
			t.Fatalf("constraint %s was not created", constraint)
		}
	}
	if _, err := repository.pool.Exec(ctx, `
        INSERT INTO processing_jobs (meeting_id, status)
        VALUES ('00000000-0000-0000-0000-000000000000', 'unknown')
    `); err == nil {
		t.Fatal("invalid processing status was accepted")
	}
}

func TestRepositoryCompleteFlowAndSearch(t *testing.T) {
	repository := resetIntegrationRepository(t)
	ctx := context.Background()
	user, err := repository.GetOrCreateUser(ctx, "telegram:owner")
	if err != nil {
		t.Fatalf("GetOrCreateUser() error = %v", err)
	}
	again, err := repository.GetOrCreateUser(ctx, "telegram:owner")
	if err != nil || again.ID != user.ID || !again.CreatedAt.Equal(user.CreatedAt) {
		t.Fatalf("idempotent user=%+v error=%v", again, err)
	}
	file := domain.FileMetadata{
		ExternalFileID: "file-1",
		FileName:       "planning 100%.ogg",
		MIMEType:       "audio/ogg",
		SizeBytes:      42,
		Duration:       3 * time.Second,
	}
	meeting, job, err := repository.CreateMeetingWithJob(ctx, user.ID, file)
	if err != nil || job.Status != domain.StatusCreated || meeting.File.Duration != file.Duration {
		t.Fatalf("CreateMeetingWithJob() meeting=%+v job=%+v error=%v", meeting, job, err)
	}
	job, err = repository.MarkProcessing(ctx, user.ID, meeting.ID)
	if err != nil || job.Status != domain.StatusProcessing || job.StartedAt == nil {
		t.Fatalf("MarkProcessing() job=%+v error=%v", job, err)
	}
	transcript, job, err := repository.SaveTranscript(ctx, user.ID, meeting.ID, "Launch is 100% ready")
	if err != nil || job.Status != domain.StatusTranscribed || transcript.Text == "" {
		t.Fatalf("SaveTranscript() transcript=%+v job=%+v error=%v", transcript, job, err)
	}
	summary, job, err := repository.SaveSummary(ctx, user.ID, meeting.ID, "Launch summary")
	if err != nil || job.Status != domain.StatusSummarized || summary.Text == "" {
		t.Fatalf("SaveSummary() summary=%+v job=%+v error=%v", summary, job, err)
	}
	job, err = repository.MarkCompleted(ctx, user.ID, meeting.ID)
	if err != nil || job.Status != domain.StatusCompleted || job.CompletedAt == nil {
		t.Fatalf("MarkCompleted() job=%+v error=%v", job, err)
	}

	records, err := repository.ListMeetings(ctx, user.ID)
	if err != nil || len(records) != 1 || records[0].Summary == nil || records[0].Job.Status != domain.StatusCompleted {
		t.Fatalf("ListMeetings() records=%+v error=%v", records, err)
	}
	results, err := repository.FindMeetings(ctx, user.ID, "%")
	if err != nil || len(results) != 1 || results[0].Record.Meeting.ID != meeting.ID {
		t.Fatalf("FindMeetings() results=%+v error=%v", results, err)
	}
}

func TestRepositoryOwnerIsolation(t *testing.T) {
	repository := resetIntegrationRepository(t)
	ctx := context.Background()
	owner, _ := repository.GetOrCreateUser(ctx, "telegram:owner")
	other, _ := repository.GetOrCreateUser(ctx, "telegram:other")
	meeting, _, err := repository.CreateMeetingWithJob(ctx, owner.ID, domain.FileMetadata{ExternalFileID: "secret", MIMEType: "audio/ogg"})
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}
	_, _ = repository.MarkProcessing(ctx, owner.ID, meeting.ID)
	_, _, _ = repository.SaveTranscript(ctx, owner.ID, meeting.ID, "private launch notes")

	checks := []func() error{
		func() error { _, err := repository.GetMeeting(ctx, other.ID, meeting.ID); return err },
		func() error { _, err := repository.GetJob(ctx, other.ID, meeting.ID); return err },
		func() error { _, err := repository.GetTranscript(ctx, other.ID, meeting.ID); return err },
		func() error { _, err := repository.GetSummary(ctx, other.ID, meeting.ID); return err },
	}
	for index, check := range checks {
		if err := check(); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ownership check %d error = %v", index, err)
		}
	}
	records, err := repository.ListMeetings(ctx, other.ID)
	if err != nil || len(records) != 0 {
		t.Fatalf("other user list=%+v error=%v", records, err)
	}
	results, err := repository.FindMeetings(ctx, other.ID, "launch")
	if err != nil || len(results) != 0 {
		t.Fatalf("other user search=%+v error=%v", results, err)
	}
}

func TestRepositoryFailureAndTransactionalRollback(t *testing.T) {
	repository := resetIntegrationRepository(t)
	ctx := context.Background()
	user, _ := repository.GetOrCreateUser(ctx, "telegram:owner")
	meeting, _, err := repository.CreateMeetingWithJob(ctx, user.ID, domain.FileMetadata{ExternalFileID: "file", MIMEType: "audio/ogg"})
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}
	if _, _, err := repository.SaveTranscript(ctx, user.ID, meeting.ID, "too early"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("SaveTranscript() error = %v", err)
	}
	var transcriptCount int
	if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM transcripts WHERE meeting_id = $1::uuid", meeting.ID).Scan(&transcriptCount); err != nil {
		t.Fatalf("count transcripts: %v", err)
	}
	if transcriptCount != 0 {
		t.Fatalf("transaction left %d transcripts after invalid transition", transcriptCount)
	}
	failed, err := repository.MarkFailed(ctx, user.ID, meeting.ID, "speech failed")
	if err != nil || failed.Status != domain.StatusFailed || !strings.Contains(failed.Error, "speech failed") {
		t.Fatalf("MarkFailed() job=%+v error=%v", failed, err)
	}
}

func TestCreateMeetingRollsBackWhenJobInsertFails(t *testing.T) {
	repository := resetIntegrationRepository(t)
	ctx := context.Background()
	user, _ := repository.GetOrCreateUser(ctx, "telegram:owner")
	_, err := repository.pool.Exec(ctx, `
        CREATE OR REPLACE FUNCTION fail_processing_job_insert() RETURNS trigger AS $$
        BEGIN
            RAISE EXCEPTION 'forced job failure';
        END;
        $$ LANGUAGE plpgsql;
        CREATE TRIGGER force_processing_job_failure
        BEFORE INSERT ON processing_jobs
        FOR EACH ROW EXECUTE FUNCTION fail_processing_job_insert();
    `)
	if err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repository.pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS force_processing_job_failure ON processing_jobs")
		_, _ = repository.pool.Exec(context.Background(), "DROP FUNCTION IF EXISTS fail_processing_job_insert()")
	})

	_, _, err = repository.CreateMeetingWithJob(ctx, user.ID, domain.FileMetadata{ExternalFileID: "rollback-file", MIMEType: "audio/ogg"})
	if !errors.Is(err, domain.ErrInfrastructure) {
		t.Fatalf("CreateMeetingWithJob() error = %v", err)
	}
	var meetings int
	if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM meetings WHERE external_file_id = 'rollback-file'").Scan(&meetings); err != nil {
		t.Fatalf("count meetings: %v", err)
	}
	if meetings != 0 {
		t.Fatalf("meeting transaction was not rolled back, count=%d", meetings)
	}
}

func TestRepositoryHonorsContextTimeout(t *testing.T) {
	repository := resetIntegrationRepository(t)
	ctx := context.Background()
	_, err := repository.GetOrCreateUser(ctx, "telegram:locked")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT id FROM users WHERE external_id = 'telegram:locked' FOR UPDATE"); err != nil {
		t.Fatalf("lock user: %v", err)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = repository.GetOrCreateUser(timeoutCtx, "telegram:locked")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetOrCreateUser() timeout error = %v", err)
	}
}

func integrationRepository(t *testing.T) *Repository {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}
	repository := &Repository{pool: pool, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(pool.Close)
	return repository
}

func resetIntegrationRepository(t *testing.T) *Repository {
	t.Helper()
	repository := integrationRepository(t)
	ctx := context.Background()
	if err := repository.MigrateUp(ctx); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}
	if _, err := repository.pool.Exec(ctx, "TRUNCATE summaries, transcripts, processing_jobs, meetings, users CASCADE"); err != nil {
		t.Fatalf("truncate test database: %v", err)
	}
	return repository
}

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_external_id_not_blank CHECK (btrim(external_id) <> '')
);

CREATE TABLE meetings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    external_file_id TEXT NOT NULL,
    file_name TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT meetings_external_file_id_not_blank CHECK (btrim(external_file_id) <> ''),
    CONSTRAINT meetings_size_non_negative CHECK (size_bytes >= 0),
    CONSTRAINT meetings_duration_non_negative CHECK (duration_ms >= 0)
);

CREATE TABLE processing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id UUID NOT NULL UNIQUE REFERENCES meetings(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'created',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT processing_jobs_status_valid CHECK (
        status IN ('created', 'processing', 'transcribed', 'summarized', 'completed', 'failed')
    ),
    CONSTRAINT processing_jobs_failed_has_error CHECK (status <> 'failed' OR btrim(error) <> '')
);

CREATE TABLE transcripts (
    meeting_id UUID PRIMARY KEY REFERENCES meetings(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transcripts_text_not_blank CHECK (btrim(text) <> '')
);

CREATE TABLE summaries (
    meeting_id UUID PRIMARY KEY REFERENCES meetings(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT summaries_text_not_blank CHECK (btrim(text) <> '')
);

CREATE INDEX idx_meetings_user_created ON meetings (user_id, created_at DESC, id DESC);
CREATE INDEX idx_meetings_user_id ON meetings (user_id, id);
CREATE INDEX idx_processing_jobs_status_created ON processing_jobs (status, created_at);
CREATE INDEX idx_transcripts_text_trgm ON transcripts USING GIN (text gin_trgm_ops);
CREATE INDEX idx_summaries_text_trgm ON summaries USING GIN (text gin_trgm_ops);

-- Lease ownership and bounded recovery state are authoritative in PostgreSQL.
ALTER TABLE processing_jobs
    ADD COLUMN next_attempt_at TIMESTAMPTZ;

ALTER TABLE job_attempts
    ADD COLUMN leased_at TIMESTAMPTZ,
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN last_renewed_at TIMESTAMPTZ;

CREATE INDEX job_attempts_expiry_idx
    ON job_attempts (lease_expires_at, id)
    WHERE state IN ('LEASED', 'RUNNING') AND lease_expires_at IS NOT NULL;

CREATE INDEX processing_jobs_retry_ready_idx
    ON processing_jobs (next_attempt_at, priority DESC, created_at, id)
    WHERE state = 'QUEUED' AND next_attempt_at IS NOT NULL;

CREATE TABLE job_dead_letters (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    attempt_id UUID NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
    lease_id UUID NOT NULL,
    worker_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    error_code TEXT NOT NULL CHECK (CHAR_LENGTH(error_code) BETWEEN 1 AND 128),
    error_message TEXT NOT NULL CHECK (CHAR_LENGTH(error_message) BETWEEN 1 AND 500),
    retryable BOOLEAN NOT NULL,
    diagnostic_ref TEXT CHECK (diagnostic_ref IS NULL OR CHAR_LENGTH(diagnostic_ref) <= 512),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    replayed_at TIMESTAMPTZ,
    replayed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (job_id, attempt_id)
);

CREATE INDEX job_dead_letters_owner_created_idx
    ON job_dead_letters (job_id, created_at DESC, id);
CREATE INDEX job_dead_letters_open_idx
    ON job_dead_letters (created_at DESC, id)
    WHERE replayed_at IS NULL;

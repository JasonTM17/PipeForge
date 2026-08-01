-- Job metadata is authoritative in PostgreSQL. Workers receive commands and
-- publish result events; they never mutate these lifecycle tables directly.
CREATE TABLE processing_jobs (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    dataset_version_id UUID NOT NULL REFERENCES dataset_versions(id),
    state TEXT NOT NULL DEFAULT 'CREATED' CHECK (state IN (
        'CREATED', 'QUEUED', 'LEASED', 'RUNNING', 'SUCCEEDED',
        'FAILED_RETRYABLE', 'FAILED_PERMANENT', 'CANCEL_REQUESTED',
        'CANCELLED', 'DEAD_LETTERED'
    )),
    operations JSONB NOT NULL CHECK (jsonb_typeof(operations) = 'array'),
    request_fingerprint TEXT NOT NULL CHECK (CHAR_LENGTH(request_fingerprint) = 64),
    idempotency_key TEXT CHECK (idempotency_key IS NULL OR CHAR_LENGTH(idempotency_key) BETWEEN 1 AND 128),
    priority SMALLINT NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 100),
    max_attempts SMALLINT NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    queued_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    cancel_requested_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_message TEXT
);

CREATE INDEX processing_jobs_queue_idx
    ON processing_jobs (priority DESC, created_at, id)
    WHERE state = 'QUEUED';
CREATE INDEX processing_jobs_owner_state_idx
    ON processing_jobs (owner_user_id, state, created_at DESC);
CREATE INDEX processing_jobs_version_idx
    ON processing_jobs (dataset_version_id, created_at DESC);

CREATE TABLE job_attempts (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    state TEXT NOT NULL DEFAULT 'CREATED' CHECK (state IN (
        'CREATED', 'LEASED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'TIMED_OUT', 'CANCELLED'
    )),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (job_id, attempt_number)
);

CREATE INDEX job_attempts_job_created_idx ON job_attempts (job_id, attempt_number DESC);

CREATE TABLE job_state_history (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    from_state TEXT,
    to_state TEXT NOT NULL,
    reason TEXT NOT NULL,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX job_state_history_job_created_idx ON job_state_history (job_id, created_at DESC);

CREATE TABLE job_idempotency_records (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL CHECK (CHAR_LENGTH(idempotency_key) BETWEEN 1 AND 128),
    request_fingerprint TEXT NOT NULL CHECK (CHAR_LENGTH(request_fingerprint) = 64),
    job_id UUID NOT NULL UNIQUE REFERENCES processing_jobs(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    UNIQUE (owner_user_id, idempotency_key)
);

CREATE TABLE job_quota_counters (
    owner_user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    queued_count INTEGER NOT NULL DEFAULT 0 CHECK (queued_count >= 0),
    active_count INTEGER NOT NULL DEFAULT 0 CHECK (active_count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

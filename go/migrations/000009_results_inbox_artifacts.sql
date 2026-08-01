-- Result events are applied transactionally and deduplicated by message_id.
-- Lease identity columns are populated by the lease phase and fence worker
-- events before any job transition or artifact promotion is accepted.
ALTER TABLE job_attempts
    ADD COLUMN lease_id UUID,
    ADD COLUMN worker_id UUID;

CREATE UNIQUE INDEX job_attempts_lease_id_idx
    ON job_attempts (lease_id)
    WHERE lease_id IS NOT NULL;

CREATE TABLE inbox_messages (
    message_id UUID PRIMARY KEY,
    message_type TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    outcome TEXT NOT NULL CHECK (outcome IN ('PROCESSING', 'APPLIED', 'DUPLICATE', 'IGNORED', 'REJECTED')),
    reason TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX inbox_messages_received_idx ON inbox_messages (received_at DESC, message_id);

CREATE TABLE job_result_projections (
    job_id UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    attempt_id UUID NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK (outcome IN ('SUCCEEDED', 'FAILED')),
    artifact_keys JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(artifact_keys) = 'array'),
    error_code TEXT,
    error_message TEXT,
    retryable BOOLEAN,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (job_id, attempt_id)
);

CREATE TABLE job_artifacts (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    job_id UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    attempt_id UUID NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
    lease_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind ~ '^[a-z0-9_-]{1,64}$'),
    object_key TEXT NOT NULL CHECK (CHAR_LENGTH(object_key) BETWEEN 1 AND 512 AND object_key ~ '^[a-z0-9/_-]+\.[a-z0-9]+$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    content_type TEXT NOT NULL CHECK (CHAR_LENGTH(content_type) BETWEEN 1 AND 128),
    checksum_sha256 TEXT NOT NULL CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    state TEXT NOT NULL DEFAULT 'STAGED' CHECK (state IN ('STAGED', 'CANONICAL', 'REJECTED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, attempt_id, object_key)
);

CREATE INDEX job_artifacts_owner_created_idx
    ON job_artifacts (owner_user_id, created_at DESC, id);
CREATE INDEX job_artifacts_job_attempt_idx
    ON job_artifacts (job_id, attempt_id, created_at DESC);

CREATE TABLE job_progress_snapshots (
    job_id UUID PRIMARY KEY REFERENCES processing_jobs(id) ON DELETE CASCADE,
    attempt_id UUID NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
    lease_id UUID NOT NULL,
    stage TEXT NOT NULL CHECK (stage ~ '^[A-Za-z0-9_-]{1,64}$'),
    processed_rows BIGINT NOT NULL CHECK (processed_rows >= 0),
    estimated_total_rows BIGINT CHECK (estimated_total_rows IS NULL OR estimated_total_rows >= 0),
    progress_percent DOUBLE PRECISION NOT NULL CHECK (progress_percent BETWEEN 0 AND 100),
    throughput DOUBLE PRECISION NOT NULL CHECK (throughput >= 0),
    updated_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

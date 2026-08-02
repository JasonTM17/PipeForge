-- Progress history is bounded per job; the latest snapshot remains the fast read path.
ALTER TABLE job_result_projections
    DROP CONSTRAINT IF EXISTS job_result_projections_outcome_check;

ALTER TABLE job_result_projections
    ADD CONSTRAINT job_result_projections_outcome_check
    CHECK (outcome IN ('SUCCEEDED', 'FAILED', 'CANCELLED'));

CREATE TABLE job_progress_history (
    id BIGSERIAL PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
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

CREATE INDEX job_progress_history_job_updated_idx
    ON job_progress_history (job_id, updated_at DESC, id DESC);

CREATE INDEX job_progress_history_attempt_updated_idx
    ON job_progress_history (attempt_id, updated_at DESC, id DESC);

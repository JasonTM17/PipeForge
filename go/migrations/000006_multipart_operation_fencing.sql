-- Fences completion workers so a retry or expiry cleanup cannot mutate a
-- session after ownership has moved to another operation.
ALTER TABLE upload_sessions
    ADD COLUMN operation_token UUID,
    ADD COLUMN completion_started_at TIMESTAMPTZ;

CREATE INDEX upload_sessions_completion_stale_idx
    ON upload_sessions (state, completion_started_at)
    WHERE state = 'COMPLETING';

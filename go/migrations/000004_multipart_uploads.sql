-- Multipart state is authoritative in PostgreSQL. The remote upload ID is
-- opaque and never accepted from clients; the API only returns scoped URLs.
CREATE TABLE upload_sessions (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    version_id UUID NOT NULL UNIQUE REFERENCES dataset_versions(id) ON DELETE CASCADE,
    upload_id TEXT NOT NULL UNIQUE,
    object_key TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL DEFAULT 'INITIATED' CHECK (state IN ('INITIATED', 'COMPLETING', 'ABORTING', 'COMPLETED', 'ABORTED', 'EXPIRED', 'FAILED')),
    original_filename TEXT NOT NULL CHECK (CHAR_LENGTH(original_filename) BETWEEN 1 AND 255),
    content_type TEXT NOT NULL,
    format TEXT NOT NULL CHECK (format IN ('CSV', 'JSONL', 'PARQUET')),
    expected_size BIGINT NOT NULL CHECK (expected_size > 0),
    expected_checksum_sha256 TEXT CHECK (expected_checksum_sha256 IS NULL OR expected_checksum_sha256 ~ '^[0-9a-f]{64}$'),
    part_size BIGINT NOT NULL CHECK (part_size > 0),
    part_count INTEGER NOT NULL CHECK (part_count BETWEEN 1 AND 10000),
    idempotency_key TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    aborted_at TIMESTAMPTZ,
    last_error TEXT
);

CREATE INDEX upload_sessions_owner_created_idx ON upload_sessions (owner_user_id, created_at DESC);
CREATE INDEX upload_sessions_expiry_idx ON upload_sessions (expires_at, state);
CREATE UNIQUE INDEX upload_sessions_active_idempotency_idx
    ON upload_sessions (owner_user_id, dataset_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND state IN ('INITIATED', 'COMPLETING', 'ABORTING', 'COMPLETED');

CREATE TABLE upload_parts (
    session_id UUID NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
    part_number INTEGER NOT NULL CHECK (part_number BETWEEN 1 AND 10000),
    etag TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, part_number)
);

CREATE INDEX upload_parts_session_created_idx ON upload_parts (session_id, created_at, part_number);

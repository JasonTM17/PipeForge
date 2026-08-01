-- Dataset metadata is authoritative in PostgreSQL. Raw bytes are stored in a
-- private MinIO bucket and referenced by server-generated object keys.
CREATE TABLE datasets (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK (CHAR_LENGTH(name) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'REGISTERED' CHECK (state IN ('REGISTERED', 'UPLOADING', 'AVAILABLE', 'PROCESSING', 'READY', 'FAILED', 'DELETED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX datasets_owner_name_active_idx ON datasets (owner_user_id, LOWER(name)) WHERE deleted_at IS NULL;
CREATE INDEX datasets_owner_created_idx ON datasets (owner_user_id, created_at DESC);
CREATE INDEX datasets_state_updated_idx ON datasets (state, updated_at);

CREATE TABLE dataset_versions (
    id UUID PRIMARY KEY,
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL CHECK (version_number > 0),
    state TEXT NOT NULL DEFAULT 'UPLOADING' CHECK (state IN ('UPLOADING', 'AVAILABLE', 'FAILED')),
    original_filename TEXT NOT NULL CHECK (CHAR_LENGTH(original_filename) BETWEEN 1 AND 255),
    content_type TEXT NOT NULL,
    format TEXT NOT NULL CHECK (format IN ('CSV', 'JSONL', 'PARQUET')),
    size_bytes BIGINT CHECK (size_bytes >= 0),
    object_key TEXT NOT NULL UNIQUE,
    checksum_sha256 TEXT CHECK (checksum_sha256 IS NULL OR checksum_sha256 ~ '^[0-9a-f]{64}$'),
    row_count BIGINT CHECK (row_count IS NULL OR row_count >= 0),
    column_count INTEGER CHECK (column_count IS NULL OR column_count >= 0),
    encoding TEXT,
    compression_type TEXT,
    source_info JSONB NOT NULL DEFAULT '{}',
    artifact_prefix TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX dataset_versions_number_idx ON dataset_versions (dataset_id, version_number);
CREATE INDEX dataset_versions_dataset_created_idx ON dataset_versions (dataset_id, created_at DESC);
CREATE INDEX dataset_versions_state_idx ON dataset_versions (state, created_at);

CREATE TABLE dataset_state_history (
    id BIGSERIAL PRIMARY KEY,
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    from_state TEXT,
    to_state TEXT NOT NULL,
    reason TEXT NOT NULL,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX dataset_state_history_dataset_created_idx ON dataset_state_history (dataset_id, created_at DESC);

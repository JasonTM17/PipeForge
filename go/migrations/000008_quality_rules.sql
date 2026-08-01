-- Quality rules are versioned metadata owned by the Go control plane. Job
-- requests copy the validated definition into processing_jobs.operations so a
-- later rule edit cannot change an already-created job.
CREATE TABLE quality_rules (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (CHAR_LENGTH(name) BETWEEN 1 AND 120 AND name !~ E'[\\r\\n]'),
    column_scope TEXT[] NOT NULL DEFAULT '{}',
    rule_type TEXT NOT NULL CHECK (rule_type IN (
        'NOT_NULL', 'UNIQUE', 'BETWEEN', 'MIN_LENGTH', 'MAX_LENGTH',
        'REGEX', 'EMAIL_FORMAT', 'DATE_FORMAT', 'ALLOWED_VALUES',
        'COLUMN_TYPE', 'ROW_COUNT_BETWEEN', 'REFERENTIAL_SET'
    )),
    configuration JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(configuration) = 'object'),
    severity TEXT NOT NULL CHECK (severity IN ('INFO', 'WARNING', 'ERROR', 'CRITICAL')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CHECK (CARDINALITY(column_scope) <= 256)
);

CREATE UNIQUE INDEX quality_rules_active_name_idx
    ON quality_rules (dataset_id, name)
    WHERE deleted_at IS NULL;
CREATE INDEX quality_rules_dataset_created_idx
    ON quality_rules (dataset_id, created_at DESC, id)
    WHERE deleted_at IS NULL;
CREATE INDEX quality_rules_owner_dataset_idx
    ON quality_rules (owner_user_id, dataset_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- PipeForge foundation metadata. Domain tables are added by later migrations.
CREATE TABLE IF NOT EXISTS platform_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO platform_metadata (key, value)
VALUES ('schema_owner', 'pipeforge-control-plane')
ON CONFLICT (key) DO NOTHING;

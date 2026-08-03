CREATE TABLE processing_workers (
    worker_id UUID PRIMARY KEY,
    instance_id TEXT NOT NULL CHECK (char_length(instance_id) BETWEEN 1 AND 128),
    hostname TEXT NOT NULL CHECK (char_length(hostname) BETWEEN 1 AND 255),
    supported_operations TEXT[] NOT NULL CHECK (cardinality(supported_operations) BETWEEN 1 AND 64),
    software_version TEXT NOT NULL CHECK (char_length(software_version) BETWEEN 1 AND 64),
    status TEXT NOT NULL CHECK (status IN ('STARTING', 'READY', 'BUSY', 'DRAINING', 'UNHEALTHY', 'OFFLINE')),
    current_concurrency INTEGER NOT NULL DEFAULT 0 CHECK (current_concurrency >= 0),
    max_concurrency INTEGER NOT NULL CHECK (max_concurrency BETWEEN 1 AND 128),
    started_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    last_heartbeat_at TIMESTAMPTZ,
    last_assigned_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (current_concurrency <= max_concurrency)
);

CREATE INDEX processing_workers_schedulable_idx
    ON processing_workers (status, last_heartbeat_at DESC, last_assigned_at, worker_id);

CREATE INDEX job_attempts_active_worker_idx
    ON job_attempts (worker_id, state)
    WHERE worker_id IS NOT NULL AND state IN ('LEASED', 'RUNNING');

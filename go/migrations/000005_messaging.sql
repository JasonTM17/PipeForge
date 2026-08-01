-- Outbox rows are committed with domain mutations and published by a separate
-- worker. Delivery is at-least-once, so consumers must deduplicate message_id.
CREATE TABLE outbox_messages (
    id UUID PRIMARY KEY,
    message_id UUID NOT NULL UNIQUE,
    message_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    exchange TEXT NOT NULL,
    routing_key TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    trace_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    causation_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}',
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'IN_FLIGHT', 'PUBLISHED', 'FAILED')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    last_error TEXT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX outbox_messages_ready_idx
    ON outbox_messages (available_at, created_at, id)
    WHERE state IN ('PENDING', 'IN_FLIGHT');

CREATE INDEX outbox_messages_lease_idx
    ON outbox_messages (lease_until)
    WHERE state = 'IN_FLIGHT';

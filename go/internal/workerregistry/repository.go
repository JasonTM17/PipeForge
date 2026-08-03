package workerregistry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool   *pgxpool.Pool
	config Config
}

func NewRepository(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("worker registry requires a database pool")
	}
	if config.HeartbeatTTL == 0 {
		config.HeartbeatTTL = DefaultHeartbeatTTL
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	if config.HeartbeatTTL <= 0 || config.HeartbeatTTL > MaxHeartbeatTTL {
		return nil, errors.New("worker heartbeat TTL must be positive and bounded")
	}
	return &Repository{pool: pool, config: config}, nil
}

func (r *Repository) ApplyEnvelope(ctx context.Context, envelope queue.Envelope) error {
	if r == nil || r.pool == nil {
		return errors.New("worker registry is not configured")
	}
	switch envelope.MessageType {
	case queue.MessageWorkerRegistered:
		registration, err := ParseRegistration(envelope)
		if err != nil {
			return err
		}
		return r.ApplyRegistration(ctx, registration)
	case queue.MessageWorkerHeartbeat:
		heartbeat, err := ParseHeartbeat(envelope)
		if err != nil {
			return err
		}
		return r.ApplyHeartbeat(ctx, heartbeat)
	default:
		return fmt.Errorf("%w: unsupported message type %q", ErrInvalidEvent, envelope.MessageType)
	}
}

func (r *Repository) ApplyRegistration(ctx context.Context, registration Registration) error {
	now := r.config.Clock.Now().UTC()
	if err := validateEventTimestamp(now, registration.StartedAt, "startedAt"); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO processing_workers (
    worker_id, instance_id, hostname, supported_operations, software_version,
    status, current_concurrency, max_concurrency, started_at, observed_at,
    last_heartbeat_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, 'STARTING', 0, $6, $7, $7, NULL, $8, $8)
ON CONFLICT (worker_id) DO UPDATE SET
    instance_id = EXCLUDED.instance_id,
    hostname = EXCLUDED.hostname,
    supported_operations = EXCLUDED.supported_operations,
    software_version = EXCLUDED.software_version,
    status = 'STARTING',
    current_concurrency = 0,
    max_concurrency = EXCLUDED.max_concurrency,
    started_at = EXCLUDED.started_at,
    observed_at = EXCLUDED.observed_at,
    last_heartbeat_at = NULL,
    updated_at = $8
WHERE EXCLUDED.started_at > processing_workers.started_at`,
		registration.WorkerID, registration.InstanceID, registration.Hostname, registration.SupportedOperations,
		registration.SoftwareVersion, registration.MaxConcurrency, registration.StartedAt, now)
	if err != nil {
		return fmt.Errorf("upsert worker registration: %w", err)
	}
	return nil
}

func (r *Repository) ApplyHeartbeat(ctx context.Context, heartbeat Heartbeat) error {
	now := r.config.Clock.Now().UTC()
	if err := validateEventTimestamp(now, heartbeat.ObservedAt, "observedAt"); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `
UPDATE processing_workers
SET hostname = $3,
    supported_operations = $4,
    software_version = $5,
    status = $6,
    current_concurrency = $7,
    max_concurrency = $8,
    observed_at = $9,
    last_heartbeat_at = $10,
    updated_at = $10
WHERE worker_id = $1
  AND instance_id = $2
  AND observed_at <= $9`,
		heartbeat.WorkerID, heartbeat.InstanceID, heartbeat.Hostname, heartbeat.SupportedOperations,
		heartbeat.SoftwareVersion, heartbeat.Status, heartbeat.CurrentConcurrency,
		heartbeat.MaxConcurrency, heartbeat.ObservedAt, now)
	if err != nil {
		return fmt.Errorf("apply worker heartbeat: %w", err)
	}
	return nil
}

func validateEventTimestamp(now, observed time.Time, field string) error {
	if observed.IsZero() {
		return fmt.Errorf("%w: %s is required", ErrInvalidEvent, field)
	}
	if observed.After(now.UTC().Add(MaxFutureClockSkew)) {
		return fmt.Errorf("%w: %s exceeds the maximum future clock skew", ErrInvalidEvent, field)
	}
	return nil
}

func (r *Repository) HeartbeatTTL() time.Duration {
	if r == nil {
		return 0
	}
	return r.config.HeartbeatTTL
}

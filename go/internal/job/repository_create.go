package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxQueuedJobsPerOwner = 100

func (r *Repository) Create(ctx context.Context, command CreateCommand) (Job, bool, error) {
	fingerprint, err := ValidateCreateCommand(command)
	if err != nil {
		return Job{}, false, err
	}
	operations, err := json.Marshal(command.Operations)
	if err != nil {
		return Job{}, false, fmt.Errorf("marshal job operations: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin job creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if command.IdempotencyKey != "" {
		existing, found, lookupErr := findIdempotentJob(ctx, tx, command.OwnerUserID, command.IdempotencyKey, fingerprint)
		if lookupErr != nil {
			return Job{}, false, lookupErr
		}
		if found {
			if err := tx.Commit(ctx); err != nil {
				return Job{}, false, fmt.Errorf("commit idempotent job lookup: %w", err)
			}
			return existing, true, nil
		}
	}
	if err := lockAvailableVersion(ctx, tx, command); err != nil {
		return Job{}, false, err
	}
	if err := incrementQuota(ctx, tx, command.OwnerUserID); err != nil {
		return Job{}, false, err
	}

	now := time.Now().UTC()
	jobID, attemptID := uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO processing_jobs (
    id, owner_user_id, dataset_version_id, state, operations, request_fingerprint,
    idempotency_key, priority, max_attempts, created_at, updated_at, queued_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $10)`,
		jobID, command.OwnerUserID, command.DatasetVersionID, StateQueued, operations, fingerprint,
		nullString(command.IdempotencyKey), command.Priority, command.MaxAttempts, now); err != nil {
		return Job{}, false, fmt.Errorf("insert job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_attempts (id, job_id, attempt_number, state, created_at)
VALUES ($1, $2, 1, $3, $4)`, attemptID, jobID, AttemptCreated, now); err != nil {
		return Job{}, false, fmt.Errorf("insert initial job attempt: %w", err)
	}
	if err := recordHistory(ctx, tx, jobID, "", StateQueued, "job_created", command.ActorUserID); err != nil {
		return Job{}, false, err
	}
	if command.IdempotencyKey != "" {
		inserted, insertErr := tx.Exec(ctx, `
INSERT INTO job_idempotency_records (id, owner_user_id, idempotency_key, request_fingerprint, job_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (owner_user_id, idempotency_key) DO NOTHING`, uuid.New(), command.OwnerUserID, command.IdempotencyKey, fingerprint, jobID)
		if insertErr != nil {
			return Job{}, false, fmt.Errorf("insert job idempotency record: %w", insertErr)
		}
		if inserted.RowsAffected() == 0 {
			existing, found, lookupErr := findIdempotentJob(ctx, tx, command.OwnerUserID, command.IdempotencyKey, fingerprint)
			if lookupErr != nil {
				return Job{}, false, lookupErr
			}
			if !found {
				return Job{}, false, ErrIdempotencyConflict
			}
			_ = tx.Rollback(ctx)
			replayed, getErr := r.Get(ctx, existing.ID)
			return replayed, true, getErr
		}
	}
	envelope, err := queue.NewEnvelope(queue.MessageJobRequested, command.TraceID, valueOrDefault(command.CorrelationID, jobID.String()), valueOrDefault(command.CausationID, command.TraceID), struct {
		JobID            uuid.UUID   `json:"jobId"`
		DatasetVersionID uuid.UUID   `json:"datasetVersionId"`
		Operations       []Operation `json:"operations"`
	}{JobID: jobID, DatasetVersionID: command.DatasetVersionID, Operations: command.Operations})
	if err != nil {
		return Job{}, false, fmt.Errorf("create job requested envelope: %w", err)
	}
	if err := r.outbox.Enqueue(ctx, tx, outbox.Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType}); err != nil {
		return Job{}, false, fmt.Errorf("enqueue job requested message: %w", err)
	}
	created, err := getJobTx(ctx, tx, jobID, false)
	if err != nil {
		return Job{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit job creation: %w", err)
	}
	return created, false, nil
}

func lockAvailableVersion(ctx context.Context, tx pgx.Tx, command CreateCommand) error {
	var ownerID uuid.UUID
	var datasetState, versionState string
	var deletedAt *time.Time
	err := tx.QueryRow(ctx, `
SELECT d.owner_user_id, d.state, d.deleted_at, v.state
FROM dataset_versions v
JOIN datasets d ON d.id = v.dataset_id
WHERE v.id = $1
FOR UPDATE OF d, v`, command.DatasetVersionID).Scan(&ownerID, &datasetState, &deletedAt, &versionState)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVersionNotFound
	}
	if err != nil {
		return fmt.Errorf("lock dataset version for job: %w", err)
	}
	if ownerID != command.OwnerUserID {
		return authz.ErrForbidden
	}
	if deletedAt != nil || datasetState == "DELETED" {
		return ErrVersionUnavailable
	}
	if versionState != "AVAILABLE" {
		return ErrVersionUnavailable
	}
	return nil
}

func findIdempotentJob(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, key, fingerprint string) (Job, bool, error) {
	var jobID uuid.UUID
	var existingFingerprint string
	err := tx.QueryRow(ctx, `
SELECT job_id, request_fingerprint
FROM job_idempotency_records
WHERE owner_user_id = $1 AND idempotency_key = $2
FOR UPDATE`, ownerID, key).Scan(&jobID, &existingFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("find job idempotency record: %w", err)
	}
	if existingFingerprint != fingerprint {
		return Job{}, false, ErrIdempotencyConflict
	}
	item, err := getJobTx(ctx, tx, jobID, false)
	if err != nil {
		return Job{}, false, err
	}
	return item, true, nil
}

func incrementQuota(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID) error {
	var count int
	err := tx.QueryRow(ctx, `
INSERT INTO job_quota_counters (owner_user_id, queued_count, active_count)
VALUES ($1, 1, 0)
ON CONFLICT (owner_user_id) DO UPDATE
SET queued_count = job_quota_counters.queued_count + 1, updated_at = NOW()
WHERE job_quota_counters.queued_count < $2
RETURNING queued_count`, ownerID, maxQueuedJobsPerOwner).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrQuotaExceeded
	}
	if err != nil {
		return fmt.Errorf("increment job quota: %w", err)
	}
	return nil
}

func recordHistory(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, fromState, toState, reason string, actorID uuid.UUID) error {
	var actor any
	if actorID != uuid.Nil {
		actor = actorID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_state_history (job_id, from_state, to_state, reason, actor_user_id)
VALUES ($1, NULLIF($2, ''), $3, $4, $5)`, jobID, fromState, toState, truncateReason(reason), actor); err != nil {
		return fmt.Errorf("record job state history: %w", err)
	}
	return nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func truncateReason(value string) string {
	if value == "" {
		return "job_state_changed"
	}
	if len(value) > 200 {
		return value[:200]
	}
	return value
}

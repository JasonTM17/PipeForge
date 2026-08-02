package job

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) RequestCancel(ctx context.Context, command CancelCommand) (Job, error) {
	if command.JobID == uuid.Nil || command.ActorUserID == uuid.Nil {
		return Job{}, fmt.Errorf("%w: job and actor are required", ErrInvalidInput)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin job cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := getJobTx(ctx, tx, command.JobID, true)
	if err != nil {
		return Job{}, err
	}
	if !command.AllowCrossOwner && item.OwnerUserID != command.ActorUserID {
		return Job{}, authz.ErrForbidden
	}
	if item.State == StateCancelRequested || item.State == StateCancelled {
		if err := tx.Commit(ctx); err != nil {
			return Job{}, fmt.Errorf("commit idempotent cancellation: %w", err)
		}
		return item, nil
	}
	if err := ValidateTransition(item.State, StateCancelRequested); err != nil {
		return Job{}, fmt.Errorf("%w: cannot cancel job in %s", ErrJobState, item.State)
	}
	reason := strings.TrimSpace(command.Reason)
	if reason == "" {
		reason = "client_requested"
	}
	if len(reason) > 500 {
		return Job{}, fmt.Errorf("%w: cancellation reason is too long", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if item.State == StateQueued {
		if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, cancel_requested_at = $3, completed_at = $3, finished_at = $3, next_attempt_at = NULL, updated_at = $3
WHERE id = $1`, item.ID, StateCancelled, now); err != nil {
			return Job{}, fmt.Errorf("cancel queued job: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE job_attempts
SET state = $2, finished_at = $3
WHERE id = (
    SELECT id FROM job_attempts
    WHERE job_id = $1
    ORDER BY attempt_number DESC, id DESC
    LIMIT 1
)`, item.ID, AttemptCancelled, now); err != nil {
			return Job{}, fmt.Errorf("cancel queued job attempt: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO job_result_projections (job_id, attempt_id, outcome, artifact_keys, error_code, error_message, retryable)
SELECT $1, id, 'CANCELLED', '[]'::jsonb, 'CANCELLED', $2, FALSE
FROM job_attempts
WHERE job_id = $1
ORDER BY attempt_number DESC, id DESC
LIMIT 1
ON CONFLICT (job_id, attempt_id) DO NOTHING`, item.ID, reason); err != nil {
			return Job{}, fmt.Errorf("record queued cancellation result: %w", err)
		}
		if err := decrementQuota(ctx, tx, item.OwnerUserID); err != nil {
			return Job{}, err
		}
		if err := recordHistory(ctx, tx, item.ID, item.State, StateCancelled, "cancelled_before_lease", command.ActorUserID); err != nil {
			return Job{}, err
		}
		updated, err := getJobTx(ctx, tx, item.ID, false)
		if err != nil {
			return Job{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Job{}, fmt.Errorf("commit queued job cancellation: %w", err)
		}
		return updated, nil
	}
	target, err := currentCancellationTarget(ctx, tx, item.ID)
	if err != nil {
		return Job{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, cancel_requested_at = $3, updated_at = $3
WHERE id = $1`, item.ID, StateCancelRequested, now); err != nil {
		return Job{}, fmt.Errorf("request job cancellation: %w", err)
	}
	if err := recordHistory(ctx, tx, item.ID, item.State, StateCancelRequested, "cancel_requested", command.ActorUserID); err != nil {
		return Job{}, err
	}
	if err := enqueueCancel(ctx, tx, r.outbox, item, target, reason, command); err != nil {
		return Job{}, err
	}
	updated, err := getJobTx(ctx, tx, item.ID, false)
	if err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit job cancellation: %w", err)
	}
	return updated, nil
}

type cancellationTarget struct {
	AttemptID uuid.UUID
	LeaseID   uuid.UUID
}

func currentCancellationTarget(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) (cancellationTarget, error) {
	var target cancellationTarget
	var attemptState string
	var leaseID *uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT id, lease_id, state
FROM job_attempts
WHERE job_id = $1
ORDER BY attempt_number DESC, id DESC
LIMIT 1
FOR UPDATE`, jobID).Scan(&target.AttemptID, &leaseID, &attemptState); err != nil {
		return cancellationTarget{}, fmt.Errorf("lock current job attempt for cancellation: %w", err)
	}
	if (attemptState != AttemptLeased && attemptState != AttemptRunning) || leaseID == nil || *leaseID == uuid.Nil {
		return cancellationTarget{}, fmt.Errorf("%w: active job has no cancellable lease", ErrJobState)
	}
	target.LeaseID = *leaseID
	return target, nil
}

func (r *Repository) RequestRetry(ctx context.Context, command RetryCommand) (Job, error) {
	if command.JobID == uuid.Nil || command.ActorUserID == uuid.Nil {
		return Job{}, fmt.Errorf("%w: job and actor are required", ErrInvalidInput)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin job retry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := getJobTx(ctx, tx, command.JobID, true)
	if err != nil {
		return Job{}, err
	}
	if !command.AllowCrossOwner && item.OwnerUserID != command.ActorUserID {
		return Job{}, authz.ErrForbidden
	}
	if item.State == StateQueued {
		if err := tx.Commit(ctx); err != nil {
			return Job{}, fmt.Errorf("commit idempotent retry: %w", err)
		}
		return item, nil
	}
	if item.State != StateFailedRetryable && item.State != StateFailedPermanent && item.State != StateDeadLettered {
		return Job{}, fmt.Errorf("%w: cannot retry job in %s", ErrJobState, item.State)
	}
	if err := incrementQuota(ctx, tx, item.OwnerUserID); err != nil {
		return Job{}, err
	}
	var attemptNumber int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM job_attempts WHERE job_id = $1`, item.ID).Scan(&attemptNumber); err != nil {
		return Job{}, fmt.Errorf("allocate retry attempt: %w", err)
	}
	if attemptNumber > int(item.MaxAttempts) && item.State != StateDeadLettered {
		return Job{}, fmt.Errorf("%w: maximum attempts reached", ErrJobState)
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
INSERT INTO job_attempts (id, job_id, attempt_number, state, created_at)
VALUES ($1, $2, $3, $4, $5)`, uuid.New(), item.ID, attemptNumber, AttemptCreated, now); err != nil {
		return Job{}, fmt.Errorf("insert retry attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, queued_at = $3, next_attempt_at = NULL, updated_at = $3, cancel_requested_at = NULL,
    last_error_code = NULL, last_error_message = NULL
WHERE id = $1`, item.ID, StateQueued, now); err != nil {
		return Job{}, fmt.Errorf("queue job retry: %w", err)
	}
	if err := recordHistory(ctx, tx, item.ID, item.State, StateQueued, "retry_requested", command.ActorUserID); err != nil {
		return Job{}, err
	}
	if err := enqueueRequested(ctx, tx, r.outbox, item, command.TraceID, command.RequestID); err != nil {
		return Job{}, err
	}
	updated, err := getJobTx(ctx, tx, item.ID, false)
	if err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit job retry: %w", err)
	}
	return updated, nil
}

func decrementQuota(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
UPDATE job_quota_counters
SET queued_count = GREATEST(queued_count - 1, 0), updated_at = NOW()
WHERE owner_user_id = $1`, ownerID); err != nil {
		return fmt.Errorf("decrement job quota: %w", err)
	}
	return nil
}

func enqueueCancel(ctx context.Context, tx pgx.Tx, writer outbox.Enqueuer, item Job, target cancellationTarget, reason string, command CancelCommand) error {
	traceID := valueOrDefault(command.TraceID, item.ID.String())
	envelope, err := queue.NewEnvelope(queue.MessageJobCancel, traceID, item.ID.String(), valueOrDefault(command.RequestID, traceID), struct {
		JobID     uuid.UUID `json:"jobId"`
		AttemptID uuid.UUID `json:"attemptId"`
		LeaseID   uuid.UUID `json:"leaseId"`
		Reason    string    `json:"reason"`
	}{JobID: item.ID, AttemptID: target.AttemptID, LeaseID: target.LeaseID, Reason: reason})
	if err != nil {
		return fmt.Errorf("create cancellation envelope: %w", err)
	}
	return enqueueEnvelope(ctx, tx, writer, envelope)
}

func enqueueRequested(ctx context.Context, tx pgx.Tx, writer outbox.Enqueuer, item Job, traceID, causationID string) error {
	traceID = valueOrDefault(traceID, item.ID.String())
	envelope, err := queue.NewEnvelope(queue.MessageJobRequested, traceID, item.ID.String(), valueOrDefault(causationID, traceID), struct {
		JobID            uuid.UUID   `json:"jobId"`
		DatasetVersionID uuid.UUID   `json:"datasetVersionId"`
		Operations       []Operation `json:"operations"`
	}{JobID: item.ID, DatasetVersionID: item.DatasetVersionID, Operations: item.Operations})
	if err != nil {
		return fmt.Errorf("create retry envelope: %w", err)
	}
	return enqueueEnvelope(ctx, tx, writer, envelope)
}

func enqueueEnvelope(ctx context.Context, tx pgx.Tx, writer outbox.Enqueuer, envelope queue.Envelope) error {
	message := outbox.Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType}
	if err := message.Validate(); err != nil {
		return fmt.Errorf("validate job outbox message: %w", err)
	}
	return writer.Enqueue(ctx, tx, message)
}

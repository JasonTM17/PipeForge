package result

import (
	"context"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) applyEvent(ctx context.Context, tx pgx.Tx, envelope queue.Envelope, event any) (string, string, error) {
	switch typed := event.(type) {
	case *StartedEvent:
		return r.applyStarted(ctx, tx, envelope, *typed)
	case *ProgressedEvent:
		return r.applyProgressed(ctx, tx, *typed)
	case *SucceededEvent:
		return r.applySucceeded(ctx, tx, *typed)
	case *FailedEvent:
		return r.applyFailed(ctx, tx, *typed)
	case *CancelledEvent:
		return r.applyCancelled(ctx, tx, *typed)
	case *ArtifactCreatedEvent:
		return r.applyArtifact(ctx, tx, *typed)
	default:
		return OutcomeIgnored, "unsupported_result_event", nil
	}
}

func (r *Repository) applyStarted(ctx context.Context, tx pgx.Tx, _ queue.Envelope, event StartedEvent) (string, string, error) {
	item, err := lockAttempt(ctx, tx, event.JobID, event.AttemptID)
	if err != nil {
		return "", "", err
	}
	if item.JobID == uuid.Nil {
		return OutcomeIgnored, "attempt_not_found", nil
	}
	if item.AttemptNumber != event.AttemptNumber || !matchesLease(item, event.LeaseID, event.WorkerID) {
		return OutcomeIgnored, "stale_or_wrong_lease", nil
	}
	if (item.JobState != job.StateLeased && item.JobState != job.StateRunning) || (item.AttemptState != job.AttemptLeased && item.AttemptState != job.AttemptRunning) {
		return OutcomeIgnored, "result_before_start_or_terminal_attempt", nil
	}
	now := time.Now().UTC()
	if item.AttemptState == job.AttemptLeased {
		if _, err := tx.Exec(ctx, `UPDATE job_attempts SET state = $2, started_at = COALESCE(started_at, $3) WHERE id = $1`, event.AttemptID, job.AttemptRunning, now); err != nil {
			return "", "", fmt.Errorf("mark attempt running: %w", err)
		}
	}
	if item.JobState == job.StateLeased {
		if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state = $2, started_at = COALESCE(started_at, $3), updated_at = $3 WHERE id = $1`, event.JobID, job.StateRunning, now); err != nil {
			return "", "", fmt.Errorf("mark job running: %w", err)
		}
		if err := recordHistory(ctx, tx, event.JobID, job.StateLeased, job.StateRunning, "worker_started"); err != nil {
			return "", "", err
		}
	}
	return OutcomeApplied, "started", nil
}

func (r *Repository) applyProgressed(ctx context.Context, tx pgx.Tx, event ProgressedEvent) (string, string, error) {
	item, err := lockAttempt(ctx, tx, event.JobID, event.AttemptID)
	if err != nil {
		return "", "", err
	}
	if item.JobID == uuid.Nil {
		return OutcomeIgnored, "attempt_not_found", nil
	}
	if !matchesLease(item, event.LeaseID, uuid.Nil) {
		return OutcomeIgnored, "stale_or_wrong_lease", nil
	}
	if item.JobState != job.StateRunning || item.AttemptState != job.AttemptRunning {
		return OutcomeIgnored, "progress_for_non_running_attempt", nil
	}
	result, err := tx.Exec(ctx, `
INSERT INTO job_progress_snapshots (job_id, attempt_id, lease_id, stage, processed_rows, estimated_total_rows, progress_percent, throughput, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (job_id) DO UPDATE SET attempt_id = EXCLUDED.attempt_id, lease_id = EXCLUDED.lease_id,
    stage = EXCLUDED.stage, processed_rows = EXCLUDED.processed_rows, estimated_total_rows = EXCLUDED.estimated_total_rows,
    progress_percent = EXCLUDED.progress_percent, throughput = EXCLUDED.throughput, updated_at = EXCLUDED.updated_at,
    received_at = NOW()
WHERE job_progress_snapshots.attempt_id <> EXCLUDED.attempt_id
   OR (EXCLUDED.updated_at > job_progress_snapshots.updated_at
       AND EXCLUDED.processed_rows >= job_progress_snapshots.processed_rows
       AND EXCLUDED.progress_percent >= job_progress_snapshots.progress_percent)`, event.JobID, event.AttemptID, event.LeaseID, event.Stage, event.ProcessedRows, event.EstimatedTotalRows, event.ProgressPercent, event.Throughput, event.UpdatedAt)
	if err != nil {
		return "", "", fmt.Errorf("persist job progress: %w", err)
	}
	if result.RowsAffected() == 0 {
		return OutcomeIgnored, "stale_progress", nil
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_progress_history (job_id, attempt_id, lease_id, stage, processed_rows, estimated_total_rows, progress_percent, throughput, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, event.JobID, event.AttemptID, event.LeaseID, event.Stage, event.ProcessedRows, event.EstimatedTotalRows, event.ProgressPercent, event.Throughput, event.UpdatedAt); err != nil {
		return "", "", fmt.Errorf("persist progress history: %w", err)
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM job_progress_history
WHERE job_id = $1
  AND id NOT IN (
      SELECT id FROM job_progress_history
      WHERE job_id = $1
      ORDER BY updated_at DESC, id DESC
      LIMIT 100
  )`, event.JobID); err != nil {
		return "", "", fmt.Errorf("trim progress history: %w", err)
	}
	return OutcomeApplied, "progressed", nil
}

func (r *Repository) applySucceeded(ctx context.Context, tx pgx.Tx, event SucceededEvent) (string, string, error) {
	item, err := lockAttempt(ctx, tx, event.JobID, event.AttemptID)
	if err != nil {
		return "", "", err
	}
	if item.JobID == uuid.Nil {
		return OutcomeIgnored, "attempt_not_found", nil
	}
	if !matchesLease(item, event.LeaseID, event.WorkerID) {
		return OutcomeIgnored, "stale_or_wrong_lease", nil
	}
	if item.JobState != job.StateRunning || item.AttemptState != job.AttemptRunning {
		return OutcomeIgnored, "result_before_start_or_terminal_attempt", nil
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state = $2, completed_at = $3, finished_at = $3, updated_at = $3 WHERE id = $1`, event.JobID, job.StateSucceeded, now); err != nil {
		return "", "", fmt.Errorf("mark job succeeded: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET state = $2, finished_at = $3 WHERE id = $1`, event.AttemptID, job.AttemptSucceeded, now); err != nil {
		return "", "", fmt.Errorf("mark attempt succeeded: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = GREATEST(active_count - 1, 0), updated_at = $2 WHERE owner_user_id = $1`, item.OwnerUserID, now); err != nil {
		return "", "", fmt.Errorf("update succeeded job quota: %w", err)
	}
	if err := recordHistory(ctx, tx, event.JobID, job.StateRunning, job.StateSucceeded, "worker_succeeded"); err != nil {
		return "", "", err
	}
	if err := insertResultProjection(ctx, tx, event.JobID, event.AttemptID, "SUCCEEDED", event.Artifacts, nil); err != nil {
		return "", "", err
	}
	if len(event.Artifacts) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE job_artifacts SET state = 'CANONICAL' WHERE job_id = $1 AND attempt_id = $2 AND object_key = ANY($3::text[])`, event.JobID, event.AttemptID, event.Artifacts); err != nil {
			return "", "", fmt.Errorf("promote result artifacts: %w", err)
		}
	}
	return OutcomeApplied, "succeeded", nil
}

func (r *Repository) applyFailed(ctx context.Context, tx pgx.Tx, event FailedEvent) (string, string, error) {
	item, err := lockAttempt(ctx, tx, event.JobID, event.AttemptID)
	if err != nil {
		return "", "", err
	}
	if item.JobID == uuid.Nil {
		return OutcomeIgnored, "attempt_not_found", nil
	}
	if !matchesLease(item, event.LeaseID, event.WorkerID) {
		return OutcomeIgnored, "stale_or_wrong_lease", nil
	}
	if item.JobState != job.StateRunning || item.AttemptState != job.AttemptRunning {
		return OutcomeIgnored, "result_before_start_or_terminal_attempt", nil
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET state = $2, finished_at = $3, error_code = $4, error_message = $5, retryable = $6 WHERE id = $1`, event.AttemptID, job.AttemptFailed, now, truncateText(event.Error.Code, 128), truncateText(event.Error.Message, 500), event.Error.Retryable); err != nil {
		return "", "", fmt.Errorf("mark attempt failed: %w", err)
	}
	if err := insertResultProjection(ctx, tx, event.JobID, event.AttemptID, "FAILED", nil, &event.Error); err != nil {
		return "", "", err
	}

	if event.Error.Retryable && item.AttemptNumber < item.MaxAttempts {
		if r.outbox == nil {
			return "", "", fmt.Errorf("retryable result requires an outbox writer")
		}
		nextAttempt := item.AttemptNumber + 1
		policy := r.retryPolicy
		if policy.MaxAttempts < item.MaxAttempts {
			policy.MaxAttempts = item.MaxAttempts
		}
		delay, err := policy.Delay(nextAttempt, nil)
		if err != nil {
			return "", "", fmt.Errorf("calculate result retry delay: %w", err)
		}
		nextAt := now.Add(delay)
		nextAttemptID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO job_attempts (id, job_id, attempt_number, state, created_at) VALUES ($1, $2, $3, $4, $5)`, nextAttemptID, event.JobID, nextAttempt, job.AttemptCreated, now); err != nil {
			return "", "", fmt.Errorf("create result retry attempt: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, next_attempt_at = $3, finished_at = NULL, updated_at = $4,
    last_error_code = $5, last_error_message = $6
WHERE id = $1`, event.JobID, job.StateQueued, nextAt, now, truncateText(event.Error.Code, 128), truncateText(event.Error.Message, 500)); err != nil {
			return "", "", fmt.Errorf("queue result retry: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = GREATEST(active_count - 1, 0), queued_count = queued_count + 1, updated_at = $2 WHERE owner_user_id = $1`, item.OwnerUserID, now); err != nil {
			return "", "", fmt.Errorf("update result retry quota: %w", err)
		}
		if err := recordHistory(ctx, tx, event.JobID, job.StateRunning, job.StateQueued, "worker_failed_retry_scheduled"); err != nil {
			return "", "", err
		}
		if err := enqueueResultRetry(ctx, tx, r.outbox, item, nextAt); err != nil {
			return "", "", err
		}
		return OutcomeApplied, "retry_scheduled", nil
	}

	state := job.StateFailedPermanent
	reason := "worker_failed"
	if event.Error.Retryable {
		state = job.StateDeadLettered
		reason = "retry_exhausted_dead_lettered"
		if _, err := tx.Exec(ctx, `
INSERT INTO job_dead_letters (id, job_id, attempt_id, lease_id, worker_id, attempt_number, error_code, error_message, retryable, diagnostic_ref)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE, $9)
ON CONFLICT (job_id, attempt_id) DO NOTHING`, uuid.New(), event.JobID, event.AttemptID, event.LeaseID, event.WorkerID, item.AttemptNumber, truncateText(event.Error.Code, 128), truncateText(event.Error.Message, 500), event.Error.DiagnosticRef); err != nil {
			return "", "", fmt.Errorf("persist exhausted result dead-letter: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state = $2, next_attempt_at = NULL, finished_at = $3, updated_at = $3, last_error_code = $4, last_error_message = $5 WHERE id = $1`, event.JobID, state, now, truncateText(event.Error.Code, 128), truncateText(event.Error.Message, 500)); err != nil {
		return "", "", fmt.Errorf("mark job failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = GREATEST(active_count - 1, 0), updated_at = $2 WHERE owner_user_id = $1`, item.OwnerUserID, now); err != nil {
		return "", "", fmt.Errorf("update failed job quota: %w", err)
	}
	if err := recordHistory(ctx, tx, event.JobID, job.StateRunning, state, reason); err != nil {
		return "", "", err
	}
	return OutcomeApplied, reason, nil
}

func (r *Repository) applyCancelled(ctx context.Context, tx pgx.Tx, event CancelledEvent) (string, string, error) {
	item, err := lockAttempt(ctx, tx, event.JobID, event.AttemptID)
	if err != nil {
		return "", "", err
	}
	if item.JobID == uuid.Nil {
		return OutcomeIgnored, "attempt_not_found", nil
	}
	if !matchesLease(item, event.LeaseID, event.WorkerID) {
		return OutcomeIgnored, "stale_or_wrong_lease", nil
	}
	if item.JobState != job.StateCancelRequested || (item.AttemptState != job.AttemptLeased && item.AttemptState != job.AttemptRunning) {
		return OutcomeIgnored, "cancellation_for_non_requested_attempt", nil
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET state = $2, finished_at = $3, error_code = NULL, error_message = NULL WHERE id = $1`, event.AttemptID, job.AttemptCancelled, now); err != nil {
		return "", "", fmt.Errorf("mark attempt cancelled: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state = $2, completed_at = $3, finished_at = $3, updated_at = $3, last_error_code = NULL, last_error_message = NULL WHERE id = $1`, event.JobID, job.StateCancelled, now); err != nil {
		return "", "", fmt.Errorf("mark job cancelled: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = GREATEST(active_count - 1, 0), updated_at = $2 WHERE owner_user_id = $1`, item.OwnerUserID, now); err != nil {
		return "", "", fmt.Errorf("update cancelled job quota: %w", err)
	}
	if err := insertResultProjection(ctx, tx, event.JobID, event.AttemptID, "CANCELLED", nil, &FailureInfo{Code: "CANCELLED", Message: truncateText(event.Reason, 500), Retryable: false}); err != nil {
		return "", "", err
	}
	if err := recordHistory(ctx, tx, event.JobID, job.StateCancelRequested, job.StateCancelled, "worker_cancelled"); err != nil {
		return "", "", err
	}
	return OutcomeApplied, "cancelled", nil
}

func enqueueResultRetry(ctx context.Context, tx pgx.Tx, writer outbox.Enqueuer, item attemptContext, availableAt time.Time) error {
	traceID := item.JobID.String()
	envelope, err := queue.NewEnvelope(queue.MessageJobRequested, traceID, traceID, traceID, struct {
		JobID            uuid.UUID       `json:"jobId"`
		DatasetVersionID uuid.UUID       `json:"datasetVersionId"`
		Operations       []job.Operation `json:"operations"`
	}{JobID: item.JobID, DatasetVersionID: item.DatasetVersionID, Operations: item.Operations})
	if err != nil {
		return fmt.Errorf("create result retry command: %w", err)
	}
	if err := writer.Enqueue(ctx, tx, outbox.Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType, AvailableAt: availableAt}); err != nil {
		return fmt.Errorf("enqueue result retry command: %w", err)
	}
	return nil
}

func (r *Repository) applyArtifact(ctx context.Context, tx pgx.Tx, event ArtifactCreatedEvent) (string, string, error) {
	item, err := lockAttempt(ctx, tx, event.JobID, event.AttemptID)
	if err != nil {
		return "", "", err
	}
	if item.JobID == uuid.Nil {
		return OutcomeIgnored, "attempt_not_found", nil
	}
	if !matchesLease(item, event.LeaseID, uuid.Nil) {
		return OutcomeIgnored, "stale_or_wrong_lease", nil
	}
	if item.JobState != job.StateLeased && item.JobState != job.StateRunning && item.JobState != job.StateSucceeded {
		return OutcomeIgnored, "artifact_for_terminal_or_invalid_job", nil
	}
	var canonical bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_result_projections WHERE job_id = $1 AND attempt_id = $2 AND outcome = 'SUCCEEDED' AND artifact_keys @> jsonb_build_array($3::text))`, event.JobID, event.AttemptID, event.ObjectKey).Scan(&canonical); err != nil {
		return "", "", fmt.Errorf("check artifact promotion: %w", err)
	}
	state := "STAGED"
	if canonical {
		state = "CANONICAL"
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_artifacts (id, owner_user_id, job_id, attempt_id, lease_id, kind, object_key, size_bytes, content_type, checksum_sha256, state)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (job_id, attempt_id, object_key) DO UPDATE SET state = CASE WHEN job_artifacts.state = 'CANONICAL' THEN 'CANONICAL' ELSE EXCLUDED.state END`, event.ArtifactID, item.OwnerUserID, event.JobID, event.AttemptID, event.LeaseID, event.Kind, event.ObjectKey, event.SizeBytes, event.ContentType, event.ChecksumSHA256, state); err != nil {
		return "", "", fmt.Errorf("persist result artifact: %w", err)
	}
	return OutcomeApplied, "artifact_created", nil
}

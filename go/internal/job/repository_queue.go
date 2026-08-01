package job

import (
	"context"
	"encoding/json"
	"fmt"
)

func (r *Repository) SelectQueued(ctx context.Context, query QueueQuery) ([]QueuedJob, error) {
	if query.Limit < 1 || query.Limit > 1000 {
		return nil, fmt.Errorf("%w: queue limit must be between 1 and 1000", ErrInvalidInput)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin queued job selection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT j.id, j.owner_user_id, j.dataset_version_id, j.state, j.operations,
       j.request_fingerprint, j.priority, j.max_attempts, j.created_at, j.updated_at,
	       j.queued_at, j.next_attempt_at, j.started_at, j.cancel_requested_at, j.completed_at, j.finished_at,
       j.last_error_code, j.last_error_message, a.id
FROM processing_jobs j
JOIN LATERAL (
    SELECT id
    FROM job_attempts
    WHERE job_id = j.id
    ORDER BY attempt_number DESC
    LIMIT 1
) a ON TRUE
WHERE j.state = $1
  AND (j.next_attempt_at IS NULL OR j.next_attempt_at <= NOW())
ORDER BY j.priority DESC, j.created_at, j.id
LIMIT $2
FOR UPDATE OF j SKIP LOCKED`, StateQueued, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("select queued jobs: %w", err)
	}
	defer rows.Close()
	items := make([]QueuedJob, 0, query.Limit)
	for rows.Next() {
		var item QueuedJob
		var operations []byte
		if err := rows.Scan(
			&item.ID, &item.OwnerUserID, &item.DatasetVersionID, &item.State, &operations,
			&item.RequestFingerprint, &item.Priority, &item.MaxAttempts, &item.CreatedAt, &item.UpdatedAt,
			&item.QueuedAt, &item.NextAttemptAt, &item.StartedAt, &item.CancelRequestedAt, &item.CompletedAt, &item.FinishedAt,
			&item.LastErrorCode, &item.LastErrorMessage, &item.AttemptID,
		); err != nil {
			return nil, fmt.Errorf("scan queued job: %w", err)
		}
		if err := json.Unmarshal(operations, &item.Operations); err != nil {
			return nil, fmt.Errorf("decode queued job operations: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queued jobs: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit queued job selection: %w", err)
	}
	return items, nil
}

var _ Store = (*Repository)(nil)
var _ QueueStore = (*Repository)(nil)

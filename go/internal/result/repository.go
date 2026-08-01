package result

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("result repository requires a database")
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Process(ctx context.Context, envelope queue.Envelope) (Outcome, error) {
	event, err := DecodeEvent(envelope)
	if err != nil {
		return Outcome{Status: OutcomeRejected, Reason: err.Error()}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Outcome{}, fmt.Errorf("begin result processing: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inserted, err := insertInbox(ctx, tx, envelope)
	if err != nil {
		return Outcome{}, err
	}
	if !inserted {
		if err := tx.Commit(ctx); err != nil {
			return Outcome{}, fmt.Errorf("commit duplicate result event: %w", err)
		}
		return Outcome{Status: OutcomeDuplicate, Reason: "message_id_already_processed"}, nil
	}

	status, reason, err := r.applyEvent(ctx, tx, envelope, event)
	if err != nil {
		return Outcome{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE inbox_messages
SET outcome = $2, reason = NULLIF($3, ''), processed_at = NOW()
WHERE message_id = $1`, envelope.MessageID, status, reason); err != nil {
		return Outcome{}, fmt.Errorf("mark result inbox message: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Outcome{}, fmt.Errorf("commit result processing: %w", err)
	}
	return Outcome{Status: status, Reason: reason}, nil
}

func insertInbox(ctx context.Context, tx pgx.Tx, envelope queue.Envelope) (bool, error) {
	result, err := tx.Exec(ctx, `
INSERT INTO inbox_messages (message_id, message_type, occurred_at, payload, outcome)
VALUES ($1, $2, $3, $4::jsonb, 'PROCESSING')
ON CONFLICT (message_id) DO NOTHING`, envelope.MessageID, envelope.MessageType, envelope.OccurredAt, []byte(envelope.Payload))
	if err != nil {
		return false, fmt.Errorf("insert result inbox message: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

type attemptContext struct {
	OwnerUserID   uuid.UUID
	JobState      string
	JobID         uuid.UUID
	AttemptID     uuid.UUID
	AttemptNumber int
	AttemptState  string
	LeaseID       *uuid.UUID
	WorkerID      *uuid.UUID
}

func lockAttempt(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID) (attemptContext, error) {
	var item attemptContext
	err := tx.QueryRow(ctx, `
SELECT j.owner_user_id, j.state, j.id, a.id, a.attempt_number, a.state, a.lease_id, a.worker_id
FROM processing_jobs j
JOIN job_attempts a ON a.job_id = j.id
WHERE j.id = $1 AND a.id = $2
FOR UPDATE`, jobID, attemptID).Scan(
		&item.OwnerUserID, &item.JobState, &item.JobID, &item.AttemptID,
		&item.AttemptNumber, &item.AttemptState, &item.LeaseID, &item.WorkerID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return attemptContext{}, nil
	}
	if err != nil {
		return attemptContext{}, fmt.Errorf("lock result attempt: %w", err)
	}
	return item, nil
}

func matchesLease(item attemptContext, leaseID, workerID uuid.UUID) bool {
	if item.LeaseID == nil || *item.LeaseID != leaseID {
		return false
	}
	if workerID == uuid.Nil {
		return true
	}
	return item.WorkerID != nil && *item.WorkerID == workerID
}

func recordHistory(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, fromState, toState, reason string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO job_state_history (job_id, from_state, to_state, reason, actor_user_id)
VALUES ($1, NULLIF($2, ''), $3, $4, NULL)`, jobID, fromState, toState, truncateText(reason, 256)); err != nil {
		return fmt.Errorf("record result state history: %w", err)
	}
	return nil
}

func insertResultProjection(ctx context.Context, tx pgx.Tx, jobID, attemptID uuid.UUID, outcome string, artifactKeys []string, failure *FailureInfo) error {
	encoded, err := json.Marshal(artifactKeys)
	if err != nil {
		return fmt.Errorf("marshal result artifact keys: %w", err)
	}
	var code, message any
	var retryable *bool
	if failure != nil {
		code = truncateText(failure.Code, 128)
		message = truncateText(failure.Message, 500)
		retryable = &failure.Retryable
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_result_projections (job_id, attempt_id, outcome, artifact_keys, error_code, error_message, retryable)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)`, jobID, attemptID, outcome, encoded, code, message, retryable); err != nil {
		return fmt.Errorf("insert result projection: %w", err)
	}
	return nil
}

func truncateText(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

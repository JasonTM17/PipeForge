package dlq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool   *pgxpool.Pool
	outbox outbox.Enqueuer
}

func NewRepository(pool *pgxpool.Pool, writer outbox.Enqueuer) (*Repository, error) {
	if pool == nil || writer == nil {
		return nil, errors.New("dead-letter repository requires database and outbox dependencies")
	}
	return &Repository{pool: pool, outbox: writer}, nil
}

func (r *Repository) List(ctx context.Context, query ListQuery) (Page, error) {
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > MaxPageSize {
		return Page{}, fmt.Errorf("%w: pagination is invalid", ErrInvalidInput)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM job_dead_letters d
JOIN processing_jobs j ON j.id = d.job_id
WHERE ($1::uuid IS NULL OR j.owner_user_id = $1)
  AND (NOT $2 OR d.replayed_at IS NULL)`, query.OwnerUserID, query.OpenOnly).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count dead letters: %w", err)
	}
	rows, err := r.pool.Query(ctx, deadLetterSelect+`WHERE ($1::uuid IS NULL OR j.owner_user_id = $1)
  AND (NOT $2 OR d.replayed_at IS NULL)
ORDER BY d.created_at DESC, d.id
LIMIT $3 OFFSET $4`, query.OwnerUserID, query.OpenOnly, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return Page{}, fmt.Errorf("list dead letters: %w", err)
	}
	defer rows.Close()
	items := make([]Record, 0, query.PageSize)
	for rows.Next() {
		item, scanErr := scanRecord(rows)
		if scanErr != nil {
			return Page{}, fmt.Errorf("scan dead letter: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate dead letters: %w", err)
	}
	return Page{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

func (r *Repository) Get(ctx context.Context, recordID uuid.UUID) (Record, error) {
	item, err := scanRecord(r.pool.QueryRow(ctx, deadLetterSelect+`WHERE d.id = $1`, recordID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get dead letter: %w", err)
	}
	return item, nil
}

func (r *Repository) Replay(ctx context.Context, command ReplayCommand) (ReplayResult, error) {
	if command.RecordID == uuid.Nil || command.ActorUserID == uuid.Nil {
		return ReplayResult{}, fmt.Errorf("%w: record and actor are required", ErrInvalidInput)
	}
	now := time.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("begin dead-letter replay: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var record Record
	var jobState string
	var maxAttempts int
	var datasetVersionID uuid.UUID
	var operations []byte
	err = tx.QueryRow(ctx, `
SELECT d.id, j.owner_user_id, d.job_id, d.attempt_id, d.lease_id, d.worker_id, d.attempt_number,
       d.error_code, d.error_message, d.retryable, d.diagnostic_ref, d.created_at, d.replayed_at, d.replayed_by,
       j.state, j.max_attempts, j.dataset_version_id, j.operations
FROM job_dead_letters d
JOIN processing_jobs j ON j.id = d.job_id
WHERE d.id = $1
FOR UPDATE OF d, j`, command.RecordID).Scan(
		&record.ID, &record.OwnerUserID, &record.JobID, &record.AttemptID, &record.LeaseID, &record.WorkerID, &record.AttemptNumber,
		&record.ErrorCode, &record.ErrorMessage, &record.Retryable, &record.DiagnosticRef, &record.CreatedAt, &record.ReplayedAt, &record.ReplayedBy,
		&jobState, &maxAttempts, &datasetVersionID, &operations,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReplayResult{}, ErrNotFound
	}
	if err != nil {
		return ReplayResult{}, fmt.Errorf("lock dead-letter replay: %w", err)
	}
	if !command.AllowCrossOwner && record.OwnerUserID != command.ActorUserID {
		return ReplayResult{}, errors.New("dead-letter owner mismatch")
	}
	if record.ReplayedAt != nil {
		return ReplayResult{}, ErrAlreadyReplayed
	}
	if jobState != job.StateDeadLettered {
		return ReplayResult{}, ErrJobState
	}
	if maxAttempts < 1 || maxAttempts > job.MaxAttempts {
		return ReplayResult{}, fmt.Errorf("%w: max attempts is invalid", ErrInvalidInput)
	}
	if err := incrementQuota(ctx, tx, record.OwnerUserID); err != nil {
		return ReplayResult{}, err
	}
	var nextAttemptNumber int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM job_attempts WHERE job_id = $1`, record.JobID).Scan(&nextAttemptNumber); err != nil {
		return ReplayResult{}, fmt.Errorf("allocate replay attempt: %w", err)
	}
	attemptID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO job_attempts (id, job_id, attempt_number, state, created_at) VALUES ($1, $2, $3, $4, $5)`, attemptID, record.JobID, nextAttemptNumber, job.AttemptCreated, now); err != nil {
		return ReplayResult{}, fmt.Errorf("insert replay attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state = $2, next_attempt_at = NULL, queued_at = $3, updated_at = $3, last_error_code = NULL, last_error_message = NULL WHERE id = $1`, record.JobID, job.StateQueued, now); err != nil {
		return ReplayResult{}, fmt.Errorf("queue replayed job: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = active_count, updated_at = $2 WHERE owner_user_id = $1`, record.OwnerUserID, now); err != nil {
		return ReplayResult{}, fmt.Errorf("update replay quota: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_dead_letters SET replayed_at = $2, replayed_by = $3 WHERE id = $1`, record.ID, now, command.ActorUserID); err != nil {
		return ReplayResult{}, fmt.Errorf("mark dead letter replayed: %w", err)
	}
	if err := recordHistory(ctx, tx, record.JobID, job.StateDeadLettered, job.StateQueued, "dead_letter_replayed", command.ActorUserID); err != nil {
		return ReplayResult{}, err
	}
	var decodedOperations []job.Operation
	if err := json.Unmarshal(operations, &decodedOperations); err != nil {
		return ReplayResult{}, fmt.Errorf("decode replay operations: %w", err)
	}
	traceID := command.TraceID
	if traceID == "" {
		traceID = record.JobID.String()
	}
	causationID := command.RequestID
	if causationID == "" {
		causationID = traceID
	}
	envelope, err := queue.NewEnvelope(queue.MessageJobRequested, traceID, record.JobID.String(), causationID, struct {
		JobID            uuid.UUID       `json:"jobId"`
		DatasetVersionID uuid.UUID       `json:"datasetVersionId"`
		Operations       []job.Operation `json:"operations"`
	}{JobID: record.JobID, DatasetVersionID: datasetVersionID, Operations: decodedOperations})
	if err != nil {
		return ReplayResult{}, fmt.Errorf("create replay command: %w", err)
	}
	if err := r.outbox.Enqueue(ctx, tx, outbox.Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType, AvailableAt: now}); err != nil {
		return ReplayResult{}, fmt.Errorf("enqueue replay command: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"deadLetterId": record.ID.String(), "attemptId": attemptID.String()})
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (actor_user_id, action, resource_type, resource_id, request_id, metadata) VALUES ($1, 'job.dead_letter_replayed', 'processing_job', $2, $3, $4::jsonb)`, command.ActorUserID, record.JobID.String(), command.RequestID, metadata); err != nil {
		return ReplayResult{}, fmt.Errorf("audit dead-letter replay: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReplayResult{}, fmt.Errorf("commit dead-letter replay: %w", err)
	}
	record.ReplayedAt = &now
	record.ReplayedBy = &command.ActorUserID
	return ReplayResult{Record: record, JobID: record.JobID, AttemptID: attemptID}, nil
}

const deadLetterSelect = `
SELECT d.id, j.owner_user_id, d.job_id, d.attempt_id, d.lease_id, d.worker_id, d.attempt_number,
       d.error_code, d.error_message, d.retryable, d.diagnostic_ref, d.created_at, d.replayed_at, d.replayed_by
FROM job_dead_letters d
JOIN processing_jobs j ON j.id = d.job_id `

func scanRecord(row interface{ Scan(...any) error }) (Record, error) {
	var item Record
	if err := row.Scan(&item.ID, &item.OwnerUserID, &item.JobID, &item.AttemptID, &item.LeaseID, &item.WorkerID, &item.AttemptNumber, &item.ErrorCode, &item.ErrorMessage, &item.Retryable, &item.DiagnosticRef, &item.CreatedAt, &item.ReplayedAt, &item.ReplayedBy); err != nil {
		return Record{}, err
	}
	return item, nil
}

func incrementQuota(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID) error {
	var count int
	err := tx.QueryRow(ctx, `
INSERT INTO job_quota_counters (owner_user_id, queued_count, active_count)
VALUES ($1, 1, 0)
ON CONFLICT (owner_user_id) DO UPDATE
SET queued_count = job_quota_counters.queued_count + 1, updated_at = NOW()
WHERE job_quota_counters.queued_count < 100
RETURNING queued_count`, ownerID).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrQuotaExceeded
	}
	if err != nil {
		return fmt.Errorf("increment replay quota: %w", err)
	}
	return nil
}

func recordHistory(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, fromState, toState, reason string, actorID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `INSERT INTO job_state_history (job_id, from_state, to_state, reason, actor_user_id) VALUES ($1, $2, $3, $4, $5)`, jobID, fromState, toState, reason, actorID); err != nil {
		return fmt.Errorf("record dead-letter history: %w", err)
	}
	return nil
}

var _ Store = (*Repository)(nil)

package lease

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
)

type leaseCandidate struct {
	JobID            uuid.UUID
	JobState         string
	NextAttemptAt    *time.Time
	DatasetVersionID uuid.UUID
	Operations       []byte
	AttemptID        uuid.UUID
	AttemptNumber    int
	AttemptState     string
	ObjectKey        string
	ContentType      string
	Format           string
	SizeBytes        *int64
}

type requestedSource struct {
	ObjectKey   string `json:"objectKey"`
	ContentType string `json:"contentType"`
	Format      string `json:"format"`
	SizeBytes   int64  `json:"sizeBytes"`
}

func (r *Repository) Acquire(ctx context.Context, command AcquireCommand) (Lease, error) {
	if r == nil || r.pool == nil || r.outbox == nil {
		return Lease{}, errors.New("lease repository is not configured")
	}
	if command.JobID == uuid.Nil {
		return Lease{}, fmt.Errorf("%w: job is required", ErrInvalidInput)
	}
	if r.config.WorkerSelector == nil {
		return Lease{}, errors.New("lease repository requires a worker selector for acquisition")
	}
	duration, err := normalizeDuration(command.Duration, r.config.LeaseDuration)
	if err != nil {
		return Lease{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Lease{}, fmt.Errorf("begin lease acquisition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	item, err := lockLeaseCandidate(ctx, tx, command.JobID)
	if err != nil {
		return Lease{}, err
	}
	now := r.config.Clock.Now().UTC()
	if item.JobState != job.StateQueued || item.AttemptState != job.AttemptCreated {
		return Lease{}, fmt.Errorf("%w: job or attempt is not queued", job.ErrJobState)
	}
	if item.NextAttemptAt != nil && item.NextAttemptAt.After(now) {
		return Lease{}, ErrLeaseNotReady
	}
	if item.SizeBytes == nil {
		return Lease{}, fmt.Errorf("lease source size is missing for dataset version %s", item.DatasetVersionID)
	}
	operationTypes, err := decodeOperationTypes(item.Operations)
	if err != nil {
		return Lease{}, err
	}
	workerID, available, err := r.config.WorkerSelector.SelectForLease(ctx, tx, operationTypes, now)
	if err != nil {
		return Lease{}, fmt.Errorf("select worker for lease: %w", err)
	}
	if !available || workerID == uuid.Nil {
		return Lease{}, ErrWorkerUnavailable
	}

	lease := Lease{
		LeaseID:        uuid.New(),
		JobID:          item.JobID,
		AttemptID:      item.AttemptID,
		WorkerID:       workerID,
		AttemptNumber:  item.AttemptNumber,
		LeasedAt:       now,
		LeaseExpiresAt: now.Add(duration),
		LastRenewedAt:  now,
	}
	if err := assignLease(ctx, tx, item, lease); err != nil {
		return Lease{}, err
	}
	if err := enqueueAcquireCommand(ctx, tx, r.outbox, item, lease, now); err != nil {
		return Lease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, fmt.Errorf("commit lease acquisition: %w", err)
	}
	return lease, nil
}

func lockLeaseCandidate(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) (leaseCandidate, error) {
	var item leaseCandidate
	err := tx.QueryRow(ctx, `
SELECT j.id, j.state, j.next_attempt_at, j.dataset_version_id, j.operations,
       a.id, a.attempt_number, a.state,
       v.object_key, v.content_type, v.format, v.size_bytes
FROM processing_jobs j
JOIN job_attempts a ON a.job_id = j.id
JOIN dataset_versions v ON v.id = j.dataset_version_id
WHERE j.id = $1
  AND a.attempt_number = (SELECT MAX(attempt_number) FROM job_attempts WHERE job_id = j.id)
	FOR UPDATE OF j, a`, jobID).Scan(
		&item.JobID, &item.JobState, &item.NextAttemptAt, &item.DatasetVersionID, &item.Operations,
		&item.AttemptID, &item.AttemptNumber, &item.AttemptState,
		&item.ObjectKey, &item.ContentType, &item.Format, &item.SizeBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return leaseCandidate{}, job.ErrJobNotFound
	}
	if err != nil {
		return leaseCandidate{}, fmt.Errorf("lock job for lease acquisition: %w", err)
	}
	return item, nil
}

func assignLease(ctx context.Context, tx pgx.Tx, item leaseCandidate, lease Lease) error {
	if _, err := tx.Exec(ctx, `
UPDATE job_attempts
SET state = $2, lease_id = $3, worker_id = $4, leased_at = $5, last_renewed_at = $5, lease_expires_at = $6
WHERE id = $1`, item.AttemptID, job.AttemptLeased, lease.LeaseID, lease.WorkerID, lease.LeasedAt, lease.LeaseExpiresAt); err != nil {
		return fmt.Errorf("assign job lease: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, next_attempt_at = NULL, updated_at = $3
WHERE id = $1`, item.JobID, job.StateLeased, lease.LeasedAt); err != nil {
		return fmt.Errorf("mark job leased: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE job_quota_counters
SET queued_count = GREATEST(queued_count - 1, 0), active_count = active_count + 1, updated_at = $2
WHERE owner_user_id = (SELECT owner_user_id FROM processing_jobs WHERE id = $1)`, item.JobID, lease.LeasedAt); err != nil {
		return fmt.Errorf("update lease quota: %w", err)
	}
	if err := recordHistory(ctx, tx, item.JobID, job.StateQueued, job.StateLeased, "lease_assigned"); err != nil {
		return err
	}
	return nil
}

func enqueueAcquireCommand(ctx context.Context, tx pgx.Tx, writer outbox.Enqueuer, item leaseCandidate, lease Lease, availableAt time.Time) error {
	message, err := newAcquireCommand(item, lease)
	if err != nil {
		return fmt.Errorf("create acquired lease command: %w", err)
	}
	message.AvailableAt = availableAt
	if err := writer.Enqueue(ctx, tx, message); err != nil {
		return fmt.Errorf("enqueue acquired lease command: %w", err)
	}
	return nil
}

func newAcquireCommand(item leaseCandidate, lease Lease) (outbox.Message, error) {
	if item.SizeBytes == nil {
		return outbox.Message{}, errors.New("lease source size is missing")
	}
	var operations []job.Operation
	if err := json.Unmarshal(item.Operations, &operations); err != nil {
		return outbox.Message{}, fmt.Errorf("decode acquired lease operations: %w", err)
	}
	envelope, err := queue.NewEnvelope(queue.MessageJobRequested, item.JobID.String(), item.JobID.String(), lease.LeaseID.String(), struct {
		JobID            uuid.UUID       `json:"jobId"`
		DatasetVersionID uuid.UUID       `json:"datasetVersionId"`
		AttemptID        uuid.UUID       `json:"attemptId"`
		LeaseID          uuid.UUID       `json:"leaseId"`
		WorkerID         uuid.UUID       `json:"workerId"`
		AttemptNumber    int             `json:"attemptNumber"`
		Operations       []job.Operation `json:"operations"`
		Source           requestedSource `json:"source"`
	}{
		JobID:            item.JobID,
		DatasetVersionID: item.DatasetVersionID,
		AttemptID:        lease.AttemptID,
		LeaseID:          lease.LeaseID,
		WorkerID:         lease.WorkerID,
		AttemptNumber:    lease.AttemptNumber,
		Operations:       operations,
		Source: requestedSource{
			ObjectKey:   item.ObjectKey,
			ContentType: item.ContentType,
			Format:      item.Format,
			SizeBytes:   *item.SizeBytes,
		},
	})
	if err != nil {
		return outbox.Message{}, err
	}
	routingKey, err := queue.WorkerRoutingKey(queue.MessageJobRequested, lease.WorkerID)
	if err != nil {
		return outbox.Message{}, err
	}
	return outbox.Message{
		ID:         envelope.MessageID,
		Envelope:   envelope,
		Exchange:   queue.CommandsExchange,
		RoutingKey: routingKey,
	}, nil
}

func decodeOperationTypes(encoded []byte) ([]string, error) {
	var operations []job.Operation
	if err := json.Unmarshal(encoded, &operations); err != nil {
		return nil, fmt.Errorf("decode lease operations for worker selection: %w", err)
	}
	if len(operations) == 0 {
		return nil, fmt.Errorf("%w: lease operations are required", ErrInvalidInput)
	}
	types := make([]string, 0, len(operations))
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if operation.Type == "" {
			return nil, fmt.Errorf("%w: lease operation type is required", ErrInvalidInput)
		}
		if _, exists := seen[operation.Type]; exists {
			continue
		}
		seen[operation.Type] = struct{}{}
		types = append(types, operation.Type)
	}
	return types, nil
}

package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool   *pgxpool.Pool
	outbox outbox.Enqueuer
}

func NewRepository(pool *pgxpool.Pool, outboxWriter outbox.Enqueuer) (*Repository, error) {
	if pool == nil || outboxWriter == nil {
		return nil, errors.New("job repository requires database and outbox dependencies")
	}
	return &Repository{pool: pool, outbox: outboxWriter}, nil
}

func (r *Repository) Get(ctx context.Context, jobID uuid.UUID) (Job, error) {
	job, err := scanJob(r.pool.QueryRow(ctx, jobSelect+`WHERE j.id = $1`, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (r *Repository) List(ctx context.Context, query ListQuery) (JobPage, error) {
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return JobPage{}, fmt.Errorf("%w: page and pageSize are invalid", ErrInvalidInput)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM processing_jobs
WHERE ($1::uuid IS NULL OR owner_user_id = $1)
  AND ($2 = '' OR state = $2)`, query.OwnerUserID, query.State).Scan(&total); err != nil {
		return JobPage{}, fmt.Errorf("count jobs: %w", err)
	}
	rows, err := r.pool.Query(ctx, jobSelect+`WHERE ($1::uuid IS NULL OR j.owner_user_id = $1)
  AND ($2 = '' OR j.state = $2)
ORDER BY j.created_at DESC, j.id
LIMIT $3 OFFSET $4`, query.OwnerUserID, query.State, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return JobPage{}, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	items := make([]Job, 0, query.PageSize)
	for rows.Next() {
		item, scanErr := scanJob(rows)
		if scanErr != nil {
			return JobPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return JobPage{}, fmt.Errorf("iterate jobs: %w", err)
	}
	return JobPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

const jobSelect = `
SELECT j.id, j.owner_user_id, j.dataset_version_id, j.state, j.operations,
       j.request_fingerprint, j.priority, j.max_attempts, j.created_at, j.updated_at,
       j.queued_at, j.started_at, j.cancel_requested_at, j.completed_at, j.finished_at,
       j.last_error_code, j.last_error_message
FROM processing_jobs j `

type scannable interface {
	Scan(...any) error
}

func scanJob(row scannable) (Job, error) {
	var item Job
	var operations []byte
	if err := row.Scan(
		&item.ID, &item.OwnerUserID, &item.DatasetVersionID, &item.State, &operations,
		&item.RequestFingerprint, &item.Priority, &item.MaxAttempts, &item.CreatedAt, &item.UpdatedAt,
		&item.QueuedAt, &item.StartedAt, &item.CancelRequestedAt, &item.CompletedAt, &item.FinishedAt,
		&item.LastErrorCode, &item.LastErrorMessage,
	); err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(operations, &item.Operations); err != nil {
		return Job{}, fmt.Errorf("decode job operations: %w", err)
	}
	return item, nil
}

func getJobTx(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, lock bool) (Job, error) {
	query := jobSelect + `WHERE j.id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	job, err := scanJob(tx.QueryRow(ctx, query, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job in transaction: %w", err)
	}
	return job, nil
}

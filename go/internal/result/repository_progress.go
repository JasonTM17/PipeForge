package result

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const progressSelect = `
SELECT p.job_id, j.owner_user_id, p.attempt_id, p.lease_id, p.stage,
       p.processed_rows, p.estimated_total_rows, p.progress_percent, p.throughput,
       p.updated_at, p.received_at
FROM job_progress_snapshots p
JOIN processing_jobs j ON j.id = p.job_id `

const progressHistorySelect = `
SELECT p.job_id, j.owner_user_id, p.attempt_id, p.lease_id, p.stage,
       p.processed_rows, p.estimated_total_rows, p.progress_percent, p.throughput,
       p.updated_at, p.received_at
FROM job_progress_history p
JOIN processing_jobs j ON j.id = p.job_id `

func (r *Repository) GetProgress(ctx context.Context, query ProgressQuery) (ProgressSnapshot, error) {
	if query.JobID == uuid.Nil {
		return ProgressSnapshot{}, fmt.Errorf("%w: job ID is required", ErrInvalidInput)
	}
	item, err := scanProgress(r.pool.QueryRow(ctx, progressSelect+`WHERE p.job_id = $1 AND ($2::uuid IS NULL OR j.owner_user_id = $2)`, query.JobID, query.OwnerUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProgressSnapshot{}, ErrProgressNotFound
	}
	if err != nil {
		return ProgressSnapshot{}, fmt.Errorf("get progress snapshot: %w", err)
	}
	return item, nil
}

func (r *Repository) ListProgress(ctx context.Context, query ProgressQuery) (ProgressPage, error) {
	if query.JobID == uuid.Nil || query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return ProgressPage{}, fmt.Errorf("%w: progress pagination is invalid", ErrInvalidInput)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM job_progress_history p
JOIN processing_jobs j ON j.id = p.job_id
WHERE p.job_id = $1 AND ($2::uuid IS NULL OR j.owner_user_id = $2)`, query.JobID, query.OwnerUserID).Scan(&total); err != nil {
		return ProgressPage{}, fmt.Errorf("count progress history: %w", err)
	}
	rows, err := r.pool.Query(ctx, progressHistorySelect+`WHERE p.job_id = $1 AND ($2::uuid IS NULL OR j.owner_user_id = $2)
ORDER BY p.updated_at DESC, p.id DESC
LIMIT $3 OFFSET $4`, query.JobID, query.OwnerUserID, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return ProgressPage{}, fmt.Errorf("list progress history: %w", err)
	}
	defer rows.Close()
	items := make([]ProgressSnapshot, 0, query.PageSize)
	for rows.Next() {
		item, scanErr := scanProgress(rows)
		if scanErr != nil {
			return ProgressPage{}, fmt.Errorf("scan progress history: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProgressPage{}, fmt.Errorf("iterate progress history: %w", err)
	}
	return ProgressPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

type progressRow interface {
	Scan(...any) error
}

func scanProgress(row progressRow) (ProgressSnapshot, error) {
	var item ProgressSnapshot
	if err := row.Scan(
		&item.JobID, &item.OwnerUserID, &item.AttemptID, &item.LeaseID, &item.Stage,
		&item.ProcessedRows, &item.EstimatedTotalRows, &item.ProgressPercent, &item.Throughput,
		&item.UpdatedAt, &item.ReceivedAt,
	); err != nil {
		return ProgressSnapshot{}, err
	}
	return item, nil
}

var _ ProgressStore = (*Repository)(nil)

package dataset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) CreateDataset(ctx context.Context, ownerID uuid.UUID, name, description string) (Dataset, error) {
	id := uuid.New()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Dataset{}, fmt.Errorf("begin dataset creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var dataset Dataset
	err = tx.QueryRow(ctx, `
INSERT INTO datasets (id, owner_user_id, name, description)
VALUES ($1, $2, $3, $4)
RETURNING id, owner_user_id, name, description, state, created_at, updated_at, deleted_at`, id, ownerID, name, description).
		Scan(&dataset.ID, &dataset.OwnerUserID, &dataset.Name, &dataset.Description, &dataset.State, &dataset.CreatedAt, &dataset.UpdatedAt, &dataset.DeletedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Dataset{}, ErrDatasetNameUsed
		}
		return Dataset{}, fmt.Errorf("insert dataset: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_state_history (dataset_id, to_state, reason, actor_user_id) VALUES ($1, $2, $3, $4)`, id, dataset.State, "dataset_created", ownerID); err != nil {
		return Dataset{}, fmt.Errorf("record dataset creation history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Dataset{}, fmt.Errorf("commit dataset creation: %w", err)
	}
	return dataset, nil
}

func (r *Repository) FindDataset(ctx context.Context, datasetID uuid.UUID) (Dataset, error) {
	var dataset Dataset
	err := r.pool.QueryRow(ctx, `SELECT id, owner_user_id, name, description, state, created_at, updated_at, deleted_at FROM datasets WHERE id = $1`, datasetID).
		Scan(&dataset.ID, &dataset.OwnerUserID, &dataset.Name, &dataset.Description, &dataset.State, &dataset.CreatedAt, &dataset.UpdatedAt, &dataset.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dataset{}, ErrDatasetNotFound
	}
	if err != nil {
		return Dataset{}, fmt.Errorf("find dataset: %w", err)
	}
	return dataset, nil
}

func (r *Repository) ListDatasets(ctx context.Context, ownerID *uuid.UUID, page, pageSize int, state, name string) (DatasetPage, error) {
	offset := (page - 1) * pageSize
	state = strings.TrimSpace(state)
	name = strings.TrimSpace(name)
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM datasets
WHERE deleted_at IS NULL
  AND ($1::uuid IS NULL OR owner_user_id = $1)
  AND ($2 = '' OR state = $2)
  AND ($3 = '' OR name ILIKE '%' || $3 || '%')`, ownerID, state, name).Scan(&total); err != nil {
		return DatasetPage{}, fmt.Errorf("count datasets: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, owner_user_id, name, description, state, created_at, updated_at, deleted_at
FROM datasets
WHERE deleted_at IS NULL
  AND ($1::uuid IS NULL OR owner_user_id = $1)
  AND ($2 = '' OR state = $2)
  AND ($3 = '' OR name ILIKE '%' || $3 || '%')
ORDER BY created_at DESC, id
LIMIT $4 OFFSET $5`, ownerID, state, name, pageSize, offset)
	if err != nil {
		return DatasetPage{}, fmt.Errorf("list datasets: %w", err)
	}
	defer rows.Close()
	pageResult := DatasetPage{Items: make([]Dataset, 0), Total: total}
	for rows.Next() {
		var dataset Dataset
		if err := rows.Scan(&dataset.ID, &dataset.OwnerUserID, &dataset.Name, &dataset.Description, &dataset.State, &dataset.CreatedAt, &dataset.UpdatedAt, &dataset.DeletedAt); err != nil {
			return DatasetPage{}, fmt.Errorf("scan dataset: %w", err)
		}
		pageResult.Items = append(pageResult.Items, dataset)
	}
	if err := rows.Err(); err != nil {
		return DatasetPage{}, fmt.Errorf("iterate datasets: %w", err)
	}
	return pageResult, nil
}

func (r *Repository) DeleteDataset(ctx context.Context, datasetID, actorID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin dataset deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM datasets WHERE id = $1 FOR UPDATE`, datasetID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDatasetNotFound
		}
		return fmt.Errorf("lock dataset for deletion: %w", err)
	}
	if state == "DELETED" {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE datasets SET state = 'DELETED', deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW() WHERE id = $1`, datasetID); err != nil {
		return fmt.Errorf("delete dataset: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO dataset_state_history (dataset_id, from_state, to_state, reason, actor_user_id) VALUES ($1, $2, 'DELETED', 'dataset_deleted', $3)`, datasetID, state, actorID); err != nil {
		return fmt.Errorf("record dataset deletion history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dataset deletion: %w", err)
	}
	return nil
}

package result

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListArtifacts(ctx context.Context, query ArtifactListQuery) (ArtifactPage, error) {
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return ArtifactPage{}, fmt.Errorf("invalid artifact pagination")
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM job_artifacts
WHERE state = 'CANONICAL'
  AND ($1::uuid IS NULL OR owner_user_id = $1)
  AND ($2::uuid IS NULL OR job_id = $2)
  AND ($3 = '' OR kind = $3)`, query.OwnerUserID, query.JobID, query.Kind).Scan(&total); err != nil {
		return ArtifactPage{}, fmt.Errorf("count artifacts: %w", err)
	}
	rows, err := r.pool.Query(ctx, artifactSelect+`WHERE state = 'CANONICAL'
  AND ($1::uuid IS NULL OR owner_user_id = $1)
  AND ($2::uuid IS NULL OR job_id = $2)
  AND ($3 = '' OR kind = $3)
ORDER BY created_at DESC, id
LIMIT $4 OFFSET $5`, query.OwnerUserID, query.JobID, query.Kind, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return ArtifactPage{}, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	items := make([]Artifact, 0, query.PageSize)
	for rows.Next() {
		item, scanErr := scanArtifact(rows)
		if scanErr != nil {
			return ArtifactPage{}, fmt.Errorf("scan artifact: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ArtifactPage{}, fmt.Errorf("iterate artifacts: %w", err)
	}
	return ArtifactPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

func (r *Repository) GetArtifact(ctx context.Context, artifactID uuid.UUID) (Artifact, error) {
	item, err := scanArtifact(r.pool.QueryRow(ctx, artifactSelect+`WHERE id = $1 AND state = 'CANONICAL'`, artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("get artifact: %w", err)
	}
	return item, nil
}

const artifactSelect = `
SELECT id, owner_user_id, job_id, attempt_id, lease_id, kind, object_key,
       size_bytes, content_type, checksum_sha256, state, created_at
FROM job_artifacts `

type artifactRow interface {
	Scan(...any) error
}

func scanArtifact(row artifactRow) (Artifact, error) {
	var item Artifact
	if err := row.Scan(&item.ID, &item.OwnerUserID, &item.JobID, &item.AttemptID, &item.LeaseID, &item.Kind, &item.ObjectKey, &item.SizeBytes, &item.ContentType, &item.ChecksumSHA256, &item.State, &item.CreatedAt); err != nil {
		return Artifact{}, err
	}
	return item, nil
}

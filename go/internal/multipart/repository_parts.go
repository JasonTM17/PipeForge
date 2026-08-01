package multipart

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) FindPart(ctx context.Context, sessionID uuid.UUID, partNumber int) (Part, error) {
	var part Part
	err := r.pool.QueryRow(ctx, `SELECT session_id, part_number, etag, size_bytes, created_at FROM upload_parts WHERE session_id = $1 AND part_number = $2`, sessionID, partNumber).
		Scan(&part.SessionID, &part.PartNumber, &part.ETag, &part.SizeBytes, &part.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Part{}, ErrPartNotFound
	}
	if err != nil {
		return Part{}, fmt.Errorf("find multipart part: %w", err)
	}
	return part, nil
}

func (r *Repository) ListParts(ctx context.Context, sessionID uuid.UUID) ([]Part, error) {
	rows, err := r.pool.Query(ctx, `SELECT session_id, part_number, etag, size_bytes, created_at FROM upload_parts WHERE session_id = $1 ORDER BY part_number`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list multipart parts: %w", err)
	}
	defer rows.Close()
	parts := make([]Part, 0)
	for rows.Next() {
		var part Part
		if err := rows.Scan(&part.SessionID, &part.PartNumber, &part.ETag, &part.SizeBytes, &part.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan multipart part: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multipart parts: %w", err)
	}
	return parts, nil
}

func (r *Repository) RegisterPart(ctx context.Context, sessionID uuid.UUID, partNumber int, etag string, size int64) (Part, error) {
	result, err := r.pool.Exec(ctx, `
INSERT INTO upload_parts (session_id, part_number, etag, size_bytes)
SELECT $1, $2, $3, $4
FROM upload_sessions
WHERE id = $1 AND state = 'INITIATED'
ON CONFLICT (session_id, part_number) DO NOTHING`, sessionID, partNumber, etag, size)
	if err != nil {
		return Part{}, fmt.Errorf("register multipart part: %w", err)
	}
	if result.RowsAffected() == 0 {
		session, sessionErr := r.FindSession(ctx, sessionID)
		if sessionErr != nil {
			return Part{}, sessionErr
		}
		if session.State != StateInitiated {
			return Part{}, ErrSessionState
		}
	}
	part, err := r.FindPart(ctx, sessionID, partNumber)
	if err != nil {
		return Part{}, err
	}
	if part.ETag != etag || part.SizeBytes != size {
		return Part{}, ErrPartConflict
	}
	return part, nil
}

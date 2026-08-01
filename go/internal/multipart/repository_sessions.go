package multipart

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/upload"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const sessionSelect = `
SELECT id, owner_user_id, dataset_id, version_id, upload_id, object_key, state,
       original_filename, content_type, format, expected_size, expected_checksum_sha256,
       part_size, part_count, idempotency_key, expires_at, created_at, updated_at,
       completed_at, aborted_at, last_error
FROM upload_sessions `

func (r *Repository) CreateSession(ctx context.Context, session Session) error {
	var expectedChecksum any
	if session.ExpectedChecksum != nil {
		expectedChecksum = *session.ExpectedChecksum
	}
	var idempotencyKey any
	if session.IdempotencyKey != "" {
		idempotencyKey = session.IdempotencyKey
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO upload_sessions (
    id, owner_user_id, dataset_id, version_id, upload_id, object_key, state,
    original_filename, content_type, format, expected_size, expected_checksum_sha256,
    part_size, part_count, idempotency_key, expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		session.ID, session.OwnerUserID, session.DatasetID, session.VersionID, session.UploadID, session.ObjectKey,
		session.State, session.OriginalFilename, session.ContentType, string(session.Format), session.ExpectedSize,
		expectedChecksum, session.PartSize, session.PartCount, idempotencyKey, session.ExpiresAt, session.CreatedAt, session.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrIdempotencyConflict
		}
		return fmt.Errorf("insert multipart session: %w", err)
	}
	return nil
}

func (r *Repository) FindSession(ctx context.Context, sessionID uuid.UUID) (Session, error) {
	return r.findSession(ctx, sessionSelect+`WHERE id = $1`, sessionID)
}

func (r *Repository) FindActiveByIdempotency(ctx context.Context, ownerID, datasetID uuid.UUID, key string) (Session, error) {
	return r.findSession(ctx, sessionSelect+`WHERE owner_user_id = $1 AND dataset_id = $2 AND idempotency_key = $3 AND state IN ('INITIATED', 'COMPLETING', 'ABORTING', 'COMPLETED')`, ownerID, datasetID, key)
}

func (r *Repository) BeginComplete(ctx context.Context, sessionID, ownerID uuid.UUID, now time.Time) (Session, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("begin multipart completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	session, err := scanSession(tx.QueryRow(ctx, sessionSelect+`WHERE id = $1 AND owner_user_id = $2 FOR UPDATE`, sessionID, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("lock multipart completion: %w", err)
	}
	switch session.State {
	case StateCompleted:
		return session, ErrSessionCompleted
	case StateAborted, StateExpired, StateFailed, StateAborting:
		return session, ErrSessionState
	case StateCompleting:
		return session, nil
	case StateInitiated:
		if !now.Before(session.ExpiresAt) {
			if _, err := tx.Exec(ctx, `UPDATE upload_sessions SET state = 'ABORTING', updated_at = $2, last_error = 'session_expired' WHERE id = $1`, sessionID, now); err != nil {
				return Session{}, fmt.Errorf("claim expired multipart session: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return Session{}, fmt.Errorf("commit expired multipart session claim: %w", err)
			}
			return Session{}, ErrSessionExpired
		}
	default:
		return Session{}, ErrSessionState
	}
	if _, err := tx.Exec(ctx, `UPDATE upload_sessions SET state = 'COMPLETING', updated_at = $2 WHERE id = $1`, sessionID, now); err != nil {
		return Session{}, fmt.Errorf("mark multipart session completing: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("commit multipart completion start: %w", err)
	}
	session.State = StateCompleting
	session.UpdatedAt = now
	return session, nil
}

func (r *Repository) BeginAbort(ctx context.Context, sessionID, ownerID uuid.UUID, now time.Time) (Session, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("begin multipart abort: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	session, err := scanSession(tx.QueryRow(ctx, sessionSelect+`WHERE id = $1 AND owner_user_id = $2 FOR UPDATE`, sessionID, ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("lock multipart abort: %w", err)
	}
	switch session.State {
	case StateCompleted:
		return session, ErrSessionCompleted
	case StateAborted, StateExpired:
		return session, nil
	case StateCompleting:
		return session, ErrSessionState
	case StateAborting:
		return session, nil
	case StateInitiated, StateFailed:
		if _, err := tx.Exec(ctx, `UPDATE upload_sessions SET state = 'ABORTING', updated_at = $2 WHERE id = $1`, sessionID, now); err != nil {
			return Session{}, fmt.Errorf("mark multipart session aborting: %w", err)
		}
	default:
		return Session{}, ErrSessionState
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("commit multipart abort start: %w", err)
	}
	session.State = StateAborting
	session.UpdatedAt = now
	return session, nil
}

func (r *Repository) ResetCompletion(ctx context.Context, sessionID uuid.UUID, lastError string) error {
	result, err := r.pool.Exec(ctx, `UPDATE upload_sessions SET state = 'INITIATED', updated_at = NOW(), last_error = $2 WHERE id = $1 AND state = 'COMPLETING'`, sessionID, truncateError(lastError))
	if err != nil {
		return fmt.Errorf("reset multipart completion: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrSessionState
	}
	return nil
}

func (r *Repository) MarkCompleted(ctx context.Context, sessionID uuid.UUID, completedAt time.Time) error {
	result, err := r.pool.Exec(ctx, `UPDATE upload_sessions SET state = 'COMPLETED', updated_at = $2, completed_at = $2, last_error = NULL WHERE id = $1 AND state = 'COMPLETING'`, sessionID, completedAt)
	if err != nil {
		return fmt.Errorf("mark multipart session completed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrSessionState
	}
	return nil
}

func (r *Repository) MarkAborted(ctx context.Context, sessionID uuid.UUID, abortedAt time.Time) error {
	result, err := r.pool.Exec(ctx, `UPDATE upload_sessions SET state = 'ABORTED', updated_at = $2, aborted_at = $2 WHERE id = $1 AND state IN ('ABORTING', 'FAILED')`, sessionID, abortedAt)
	if err != nil {
		return fmt.Errorf("mark multipart session aborted: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrSessionState
	}
	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, sessionID uuid.UUID, lastError string) error {
	result, err := r.pool.Exec(ctx, `UPDATE upload_sessions SET state = 'FAILED', updated_at = NOW(), last_error = $2 WHERE id = $1 AND state <> 'COMPLETED'`, sessionID, truncateError(lastError))
	if err != nil {
		return fmt.Errorf("mark multipart session failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrSessionState
	}
	return nil
}

func (r *Repository) ClaimExpired(ctx context.Context, now time.Time, limit int) ([]Session, error) {
	rows, err := r.pool.Query(ctx, `
WITH claimed AS (
    SELECT id
    FROM upload_sessions
    WHERE expires_at <= $1 AND state IN ('INITIATED', 'COMPLETING', 'ABORTING')
    ORDER BY expires_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
UPDATE upload_sessions s
SET state = 'ABORTING', updated_at = $1, last_error = 'session_expired'
FROM claimed
WHERE s.id = claimed.id
RETURNING s.id`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim expired multipart sessions: %w", err)
	}
	ids := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		var sessionID uuid.UUID
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan claimed multipart session: %w", err)
		}
		ids = append(ids, sessionID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed multipart sessions: %w", err)
	}
	rows.Close()
	sessions := make([]Session, 0, len(ids))
	for _, sessionID := range ids {
		session, err := r.FindSession(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (r *Repository) MarkExpired(ctx context.Context, sessionID uuid.UUID, lastError string, at time.Time) error {
	result, err := r.pool.Exec(ctx, `UPDATE upload_sessions SET state = 'EXPIRED', updated_at = $2, aborted_at = $2, last_error = $3 WHERE id = $1 AND state = 'ABORTING'`, sessionID, at, truncateError(lastError))
	if err != nil {
		return fmt.Errorf("mark multipart session expired: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrSessionState
	}
	return nil
}

func (r *Repository) findSession(ctx context.Context, query string, args ...any) (Session, error) {
	session, err := scanSession(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("find multipart session: %w", err)
	}
	return session, nil
}

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var session Session
	var format string
	var expectedChecksum, idempotencyKey, lastError *string
	err := row.Scan(
		&session.ID, &session.OwnerUserID, &session.DatasetID, &session.VersionID, &session.UploadID, &session.ObjectKey,
		&session.State, &session.OriginalFilename, &session.ContentType, &format, &session.ExpectedSize, &expectedChecksum,
		&session.PartSize, &session.PartCount, &idempotencyKey, &session.ExpiresAt, &session.CreatedAt, &session.UpdatedAt,
		&session.CompletedAt, &session.AbortedAt, &lastError,
	)
	if err != nil {
		return Session{}, err
	}
	session.Format = upload.Format(format)
	if expectedChecksum != nil {
		session.ExpectedChecksum = expectedChecksum
	}
	if idempotencyKey != nil {
		session.IdempotencyKey = *idempotencyKey
	}
	if lastError != nil {
		session.LastError = lastError
	}
	return session, nil
}

func truncateError(value string) string {
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) CreateRefreshSession(ctx context.Context, userID, familyID, tokenID uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin refresh session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_token_families (id, user_id) VALUES ($1, $2)`, familyID, userID); err != nil {
		return fmt.Errorf("insert refresh token family: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (id, family_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`, tokenID, familyID, tokenHash, expiresAt); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit refresh session: %w", err)
	}
	return nil
}

func (r *Repository) RotateRefreshToken(ctx context.Context, tokenHash []byte, newTokenID uuid.UUID, newTokenHash []byte, expiresAt time.Time) (User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tokenID, familyID, userID uuid.UUID
	var tokenExpiresAt, usedAt, tokenRevokedAt, familyRevokedAt *time.Time
	var expired bool
	row := tx.QueryRow(ctx, `
SELECT rt.id, rt.family_id, f.user_id, rt.expires_at, rt.used_at, rt.revoked_at, f.revoked_at,
       (rt.expires_at <= NOW())
FROM refresh_tokens rt
JOIN refresh_token_families f ON f.id = rt.family_id
WHERE rt.token_hash = $1
FOR UPDATE OF rt, f`, tokenHash)
	if err := row.Scan(&tokenID, &familyID, &userID, &tokenExpiresAt, &usedAt, &tokenRevokedAt, &familyRevokedAt, &expired); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrRefreshTokenInvalid
		}
		return User{}, fmt.Errorf("find refresh token: %w", err)
	}
	if familyRevokedAt != nil {
		return User{}, ErrRefreshTokenInvalid
	}
	if usedAt != nil || tokenRevokedAt != nil {
		if _, err := tx.Exec(ctx, `UPDATE refresh_token_families SET revoked_at = COALESCE(revoked_at, NOW()), revoke_reason = COALESCE(revoke_reason, 'refresh_token_reuse') WHERE id = $1`, familyID); err != nil {
			return User{}, fmt.Errorf("revoke replayed token family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return User{}, fmt.Errorf("commit replay revocation: %w", err)
		}
		return User{}, ErrRefreshTokenReplay
	}
	if expired || tokenExpiresAt == nil {
		return User{}, ErrRefreshTokenExpired
	}
	user, err := scanUser(tx.QueryRow(ctx, userSelect+`WHERE u.id = $1 GROUP BY u.id`, userID))
	if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET used_at = NOW() WHERE id = $1`, tokenID); err != nil {
		return User{}, fmt.Errorf("consume refresh token: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (id, family_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`, newTokenID, familyID, newTokenHash, expiresAt); err != nil {
		return User{}, fmt.Errorf("insert rotated refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit refresh rotation: %w", err)
	}
	return user, nil
}

func (r *Repository) RevokeRefreshTokenFamily(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `
UPDATE refresh_token_families
SET revoked_at = COALESCE(revoked_at, NOW()), revoke_reason = COALESCE(revoke_reason, 'logout')
WHERE id = (SELECT family_id FROM refresh_tokens WHERE token_hash = $1)`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke refresh token family: %w", err)
	}
	return nil
}

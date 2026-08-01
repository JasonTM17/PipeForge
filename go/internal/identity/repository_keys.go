package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) CreateAPIKey(ctx context.Context, userID uuid.UUID, name, prefix string, keyHash []byte, scopes []string) (APIKey, error) {
	var key APIKey
	err := r.pool.QueryRow(ctx, `
INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, scopes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, name, key_prefix, scopes, created_at, last_used_at, revoked_at`, uuid.New(), userID, name, prefix, keyHash, scopes).
		Scan(&key.ID, &key.Name, &key.Prefix, &key.Scopes, &key.CreatedAt, &key.LastUsedAt, &key.RevokedAt)
	if err != nil {
		return APIKey{}, fmt.Errorf("create API key: %w", err)
	}
	return key, nil
}

func (r *Repository) ListAPIKeys(ctx context.Context, userID uuid.UUID) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, name, key_prefix, scopes, created_at, last_used_at, revoked_at
FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()
	keys := make([]APIKey, 0)
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &key.Scopes, &key.CreatedAt, &key.LastUsedAt, &key.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}
	return keys, nil
}

func (r *Repository) RevokeAPIKey(ctx context.Context, userID, keyID uuid.UUID) error {
	result, err := r.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (r *Repository) AuthenticateAPIKey(ctx context.Context, value string, presentedHash []byte) (APIKeyAuthentication, error) {
	prefix, ok := auth.APIKeyPrefix(value)
	if !ok {
		return APIKeyAuthentication{}, ErrAPIKeyInvalid
	}
	var result APIKeyAuthentication
	var role string
	var storedHash []byte
	err := r.pool.QueryRow(ctx, `
SELECT k.id, k.name, k.key_prefix, k.key_hash, k.scopes, k.created_at, k.last_used_at, k.revoked_at,
       u.id, u.email, u.password_hash, u.role, u.disabled_at, u.created_at, u.updated_at,
       COALESCE(array_agg(us.scope) FILTER (WHERE us.scope IS NOT NULL), ARRAY[]::text[])
FROM api_keys k
JOIN users u ON u.id = k.user_id
LEFT JOIN user_scopes us ON us.user_id = u.id
WHERE k.key_prefix = $1 AND k.revoked_at IS NULL AND u.disabled_at IS NULL
GROUP BY k.id, u.id`, prefix).
		Scan(&result.Key.ID, &result.Key.Name, &result.Key.Prefix, &storedHash, &result.Key.Scopes, &result.Key.CreatedAt, &result.Key.LastUsedAt, &result.Key.RevokedAt,
			&result.User.ID, &result.User.Email, &result.User.PasswordHash, &role, &result.User.DisabledAt, &result.User.CreatedAt, &result.User.UpdatedAt, &result.User.Scopes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKeyAuthentication{}, ErrAPIKeyInvalid
		}
		return APIKeyAuthentication{}, fmt.Errorf("find API key: %w", err)
	}
	if subtle.ConstantTimeCompare(storedHash, presentedHash) != 1 {
		return APIKeyAuthentication{}, ErrAPIKeyInvalid
	}
	result.User.Role = auth.Role(role)
	if _, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, result.Key.ID); err != nil {
		return APIKeyAuthentication{}, fmt.Errorf("update API key usage: %w", err)
	}
	result.Scopes = append([]string(nil), result.Key.Scopes...)
	return result, nil
}

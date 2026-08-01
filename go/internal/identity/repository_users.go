package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const userSelect = `
SELECT u.id, u.email, u.password_hash, u.role, u.disabled_at, u.created_at, u.updated_at,
       COALESCE(array_agg(us.scope) FILTER (WHERE us.scope IS NOT NULL), ARRAY[]::text[])
FROM users u
LEFT JOIN user_scopes us ON us.user_id = u.id
`

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash string, role auth.Role, scopes []string) (User, error) {
	if r == nil || r.pool == nil {
		return User{}, errors.New("identity repository is not configured")
	}
	if role != auth.RoleAdmin && role != auth.RoleUser && role != auth.RoleService {
		return User{}, fmt.Errorf("invalid user role %q", role)
	}
	userID := uuid.New()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin user creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, $3, $4)`, userID, email, passwordHash, string(role))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrEmailAlreadyExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	for _, scope := range scopes {
		if _, err := tx.Exec(ctx, `INSERT INTO user_scopes (user_id, scope) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, scope); err != nil {
			return User{}, fmt.Errorf("insert user scope: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit user creation: %w", err)
	}
	return User{ID: userID, Email: email, PasswordHash: passwordHash, Role: role, Scopes: append([]string(nil), scopes...)}, nil
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	return r.findUser(ctx, userSelect+`WHERE u.email = $1 GROUP BY u.id`, email)
}

func (r *Repository) FindUserByID(ctx context.Context, userID uuid.UUID) (User, error) {
	return r.findUser(ctx, userSelect+`WHERE u.id = $1 GROUP BY u.id`, userID)
}

func (r *Repository) findUser(ctx context.Context, query string, arg any) (User, error) {
	if r == nil || r.pool == nil {
		return User{}, errors.New("identity repository is not configured")
	}
	return scanUser(r.pool.QueryRow(ctx, query, arg))
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	var role string
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &role, &user.DisabledAt, &user.CreatedAt, &user.UpdatedAt, &user.Scopes); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	user.Role = auth.Role(role)
	return user, nil
}

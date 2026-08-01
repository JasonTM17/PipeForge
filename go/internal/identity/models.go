package identity

import (
	"context"
	"errors"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/google/uuid"
)

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrEmailAlreadyExists    = errors.New("email already registered")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrRefreshTokenInvalid   = errors.New("invalid refresh token")
	ErrRefreshTokenReplay    = errors.New("refresh token replay detected")
	ErrRefreshTokenExpired   = errors.New("refresh token expired")
	ErrAPIKeyNotFound        = errors.New("API key not found")
	ErrAPIKeyInvalid         = errors.New("invalid API key")
	ErrIdentityConfiguration = errors.New("invalid identity configuration")
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         auth.Role
	Scopes       []string
	DisabledAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APIKey struct {
	ID         uuid.UUID
	Name       string
	Prefix     string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type APIKeyAuthentication struct {
	Key    APIKey
	User   User
	Scopes []string
}

type TokenPair struct {
	AccessToken       string
	RefreshToken      string
	AccessTokenExpiry time.Time
}

type AuditEvent struct {
	ActorUserID  *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   string
	RequestID    string
	Metadata     map[string]any
}

type Store interface {
	CreateUser(context.Context, string, string, auth.Role, []string) (User, error)
	FindUserByEmail(context.Context, string) (User, error)
	FindUserByID(context.Context, uuid.UUID) (User, error)
	CreateRefreshSession(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []byte, time.Time) error
	RotateRefreshToken(context.Context, []byte, uuid.UUID, []byte, time.Time) (User, error)
	RevokeRefreshTokenFamily(context.Context, []byte) error
	CreateAPIKey(context.Context, uuid.UUID, string, string, []byte, []string) (APIKey, error)
	ListAPIKeys(context.Context, uuid.UUID) ([]APIKey, error)
	RevokeAPIKey(context.Context, uuid.UUID, uuid.UUID) error
	AuthenticateAPIKey(context.Context, string, []byte) (APIKeyAuthentication, error)
	RecordAudit(context.Context, AuditEvent) error
}

package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/google/uuid"
)

type Service struct {
	Store           Store
	PasswordHasher  auth.PasswordHasher
	AccessTokens    auth.AccessTokenService
	RefreshTokenTTL time.Duration
	Now             func() time.Time
}

type CreatedAPIKey struct {
	Key       APIKey
	Plaintext string
}

func NewService(store Store, cfg config.Config) (*Service, error) {
	if store == nil || len(cfg.JWTSigningKey) < 32 || cfg.AccessTokenTTL <= 0 || cfg.RefreshTokenTTL <= 0 {
		return nil, ErrIdentityConfiguration
	}
	return &Service{
		Store:           store,
		PasswordHasher:  auth.NewPasswordHasher(),
		AccessTokens:    auth.NewAccessTokenService(cfg.JWTSigningKey, cfg.AccessTokenTTL),
		RefreshTokenTTL: cfg.RefreshTokenTTL,
		Now:             time.Now,
	}, nil
}

func (s *Service) Register(ctx context.Context, email, password, requestID string) (User, TokenPair, error) {
	if err := validateService(s); err != nil {
		return User{}, TokenPair{}, err
	}
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return User{}, TokenPair{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	passwordHash, err := s.PasswordHasher.Hash(password)
	if err != nil {
		return User{}, TokenPair{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	user, err := s.Store.CreateUser(ctx, normalizedEmail, passwordHash, auth.RoleUser, authz.DefaultUserScopes())
	if err != nil {
		return User{}, TokenPair{}, err
	}
	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	if err := s.recordAudit(ctx, user.ID, "identity.register", requestID, map[string]any{"role": string(user.Role)}); err != nil {
		return User{}, TokenPair{}, err
	}
	return user, pair, nil
}

func (s *Service) Login(ctx context.Context, email, password, requestID string) (User, TokenPair, error) {
	if err := validateService(s); err != nil {
		return User{}, TokenPair{}, err
	}
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	user, err := s.Store.FindUserByEmail(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return User{}, TokenPair{}, ErrInvalidCredentials
		}
		return User{}, TokenPair{}, err
	}
	if user.DisabledAt != nil {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	valid, err := auth.VerifyPassword(user.PasswordHash, password)
	if err != nil || !valid {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	if err := s.recordAudit(ctx, user.ID, "identity.login", requestID, nil); err != nil {
		return User{}, TokenPair{}, err
	}
	return user, pair, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken, requestID string) (User, TokenPair, error) {
	if err := validateService(s); err != nil {
		return User{}, TokenPair{}, err
	}
	if refreshToken == "" {
		return User{}, TokenPair{}, ErrRefreshTokenInvalid
	}
	newRefreshToken, newRefreshHash, err := auth.NewOpaqueToken()
	if err != nil {
		return User{}, TokenPair{}, err
	}
	newTokenID := uuid.New()
	expiresAt := s.now().Add(s.RefreshTokenTTL)
	user, err := s.Store.RotateRefreshToken(ctx, auth.HashOpaqueToken(refreshToken), newTokenID, newRefreshHash, expiresAt)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	if user.DisabledAt != nil {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	accessToken, accessExpiry, err := s.AccessTokens.Issue(user.ID)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	pair := TokenPair{AccessToken: accessToken, RefreshToken: newRefreshToken, AccessTokenExpiry: accessExpiry}
	if err := s.recordAudit(ctx, user.ID, "identity.refresh", requestID, nil); err != nil {
		return User{}, TokenPair{}, err
	}
	return user, pair, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken, requestID string) error {
	if err := validateService(s); err != nil {
		return err
	}
	if refreshToken == "" {
		return nil
	}
	if err := s.Store.RevokeRefreshTokenFamily(ctx, auth.HashOpaqueToken(refreshToken)); err != nil {
		return err
	}
	return s.Store.RecordAudit(ctx, AuditEvent{Action: "identity.logout", RequestID: requestID})
}

func (s *Service) AuthenticateBearer(ctx context.Context, token string) (auth.Principal, error) {
	if err := validateService(s); err != nil {
		return auth.Principal{}, err
	}
	userID, err := s.AccessTokens.Parse(token)
	if err != nil {
		return auth.Principal{}, ErrInvalidCredentials
	}
	user, err := s.Store.FindUserByID(ctx, userID)
	if err != nil || user.DisabledAt != nil {
		return auth.Principal{}, ErrInvalidCredentials
	}
	return principalForUser(user, auth.AuthMethodBearer), nil
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, value string) (auth.Principal, error) {
	if err := validateService(s); err != nil {
		return auth.Principal{}, err
	}
	if value == "" {
		return auth.Principal{}, ErrAPIKeyInvalid
	}
	result, err := s.Store.AuthenticateAPIKey(ctx, value, auth.HashOpaqueToken(value))
	if err != nil {
		return auth.Principal{}, err
	}
	principal := principalForUser(result.User, auth.AuthMethodAPIKey)
	principal.Scopes = intersectScopes(result.User.Scopes, result.Scopes)
	return principal, nil
}

func (s *Service) recordAudit(ctx context.Context, userID uuid.UUID, action, requestID string, metadata map[string]any) error {
	return s.Store.RecordAudit(ctx, AuditEvent{ActorUserID: &userID, Action: action, RequestID: requestID, Metadata: metadata})
}

func (s *Service) issueTokenPair(ctx context.Context, user User) (TokenPair, error) {
	refreshToken, refreshHash, err := auth.NewOpaqueToken()
	if err != nil {
		return TokenPair{}, err
	}
	accessToken, accessExpiry, err := s.AccessTokens.Issue(user.ID)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.Store.CreateRefreshSession(ctx, user.ID, uuid.New(), uuid.New(), refreshHash, s.now().Add(s.RefreshTokenTTL)); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, AccessTokenExpiry: accessExpiry}, nil
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

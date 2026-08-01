package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/google/uuid"
)

func (s *Service) CreateAPIKey(ctx context.Context, principal auth.Principal, name string, scopes []string, requestID string) (CreatedAPIKey, error) {
	if err := validateService(s); err != nil {
		return CreatedAPIKey{}, err
	}
	if principal.UserID == uuid.Nil {
		return CreatedAPIKey{}, authz.ErrUnauthenticated
	}
	validatedName, err := validateAPIKeyName(name)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	if len(scopes) == 0 {
		return CreatedAPIKey{}, authz.ErrInvalidScope
	}
	if err := authz.ValidateGrantableScopes(principal, scopes); err != nil {
		return CreatedAPIKey{}, err
	}
	plaintext, prefix, keyHash, err := auth.NewAPIKey()
	if err != nil {
		return CreatedAPIKey{}, err
	}
	key, err := s.Store.CreateAPIKey(ctx, principal.UserID, validatedName, prefix, keyHash, scopes)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	if err := s.recordAudit(ctx, principal.UserID, "identity.api_key.create", requestID, map[string]any{"keyId": key.ID.String(), "scopeCount": len(scopes)}); err != nil {
		if revokeErr := s.Store.RevokeAPIKey(ctx, principal.UserID, key.ID); revokeErr != nil {
			return CreatedAPIKey{}, errors.Join(err, revokeErr)
		}
		return CreatedAPIKey{}, err
	}
	return CreatedAPIKey{Key: key, Plaintext: plaintext}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, principal auth.Principal) ([]APIKey, error) {
	if err := validateService(s); err != nil {
		return nil, err
	}
	if principal.UserID == uuid.Nil {
		return nil, authz.ErrUnauthenticated
	}
	return s.Store.ListAPIKeys(ctx, principal.UserID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, principal auth.Principal, keyID uuid.UUID, requestID string) error {
	if err := validateService(s); err != nil {
		return err
	}
	if principal.UserID == uuid.Nil {
		return authz.ErrUnauthenticated
	}
	if keyID == uuid.Nil {
		return ErrAPIKeyNotFound
	}
	if err := s.Store.RevokeAPIKey(ctx, principal.UserID, keyID); err != nil {
		return err
	}
	return s.recordAudit(ctx, principal.UserID, "identity.api_key.revoke", requestID, map[string]any{"keyId": keyID.String()})
}

func (s *Service) String() string {
	if s == nil {
		return "identity service <nil>"
	}
	return fmt.Sprintf("identity service refresh_ttl=%s", s.RefreshTokenTTL)
}

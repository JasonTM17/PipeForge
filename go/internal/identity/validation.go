package identity

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
)

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" || len(email) > 254 {
		return "", errors.New("email must be between 1 and 254 bytes")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", errors.New("email must be a valid address")
	}
	return email, nil
}

func validateAPIKeyName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "\r\n") {
		return "", errors.New("API key name must be between 1 and 100 bytes")
	}
	return name, nil
}

func principalForUser(user User, method string) auth.Principal {
	return auth.Principal{
		UserID:     user.ID,
		Role:       user.Role,
		Scopes:     append([]string(nil), user.Scopes...),
		AuthMethod: method,
	}
}

func intersectScopes(left, right []string) []string {
	allowed := make(map[string]struct{}, len(right))
	for _, scope := range right {
		allowed[scope] = struct{}{}
	}
	result := make([]string, 0, len(left))
	for _, scope := range left {
		if _, ok := allowed[scope]; ok {
			result = append(result, scope)
		}
	}
	return result
}

func validateService(service *Service) error {
	if service == nil || service.Store == nil || service.RefreshTokenTTL <= 0 {
		return ErrIdentityConfiguration
	}
	return nil
}

func invalidCredentialError(err error) error {
	if err == nil {
		return ErrInvalidCredentials
	}
	return fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
}

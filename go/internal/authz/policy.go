package authz

import (
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/google/uuid"
)

const (
	ScopeDatasetsRead  = "datasets:read"
	ScopeDatasetsWrite = "datasets:write"
	ScopeJobsRead      = "jobs:read"
	ScopeJobsWrite     = "jobs:write"
	ScopeArtifactsRead = "artifacts:read"
)

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("permission denied")
	ErrInvalidScope    = errors.New("invalid scope")
)

var knownScopes = map[string]struct{}{
	ScopeDatasetsRead:  {},
	ScopeDatasetsWrite: {},
	ScopeJobsRead:      {},
	ScopeJobsWrite:     {},
	ScopeArtifactsRead: {},
}

var defaultUserScopes = []string{
	ScopeDatasetsRead,
	ScopeDatasetsWrite,
	ScopeJobsRead,
	ScopeJobsWrite,
	ScopeArtifactsRead,
}

func DefaultUserScopes() []string {
	return append([]string(nil), defaultUserScopes...)
}

func IsKnownScope(scope string) bool {
	_, ok := knownScopes[scope]
	return ok
}

func HasScope(principal auth.Principal, scope string) bool {
	if principal.UserID == uuid.Nil || !IsKnownScope(scope) {
		return false
	}
	if principal.Role == auth.RoleAdmin {
		return true
	}
	for _, granted := range principal.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

func RequireScope(principal auth.Principal, scope string) error {
	if principal.UserID == uuid.Nil {
		return ErrUnauthenticated
	}
	if !HasScope(principal, scope) {
		return fmt.Errorf("%w: %s", ErrForbidden, scope)
	}
	return nil
}

func CanAccessOwner(principal auth.Principal, ownerID uuid.UUID, scope string) bool {
	if ownerID == uuid.Nil || principal.UserID == uuid.Nil || !HasScope(principal, scope) {
		return false
	}
	return principal.Role == auth.RoleAdmin || principal.UserID == ownerID
}

func ValidateGrantableScopes(principal auth.Principal, requested []string) error {
	seen := make(map[string]struct{}, len(requested))
	for _, scope := range requested {
		if _, duplicate := seen[scope]; duplicate {
			return fmt.Errorf("%w: duplicate %s", ErrInvalidScope, scope)
		}
		seen[scope] = struct{}{}
		if !IsKnownScope(scope) || !HasScope(principal, scope) {
			return fmt.Errorf("%w: %s", ErrInvalidScope, scope)
		}
	}
	return nil
}

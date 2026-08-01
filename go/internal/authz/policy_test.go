package authz

import (
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/google/uuid"
)

func TestOwnerAndRolePolicies(t *testing.T) {
	ownerID := uuid.New()
	user := auth.Principal{UserID: ownerID, Role: auth.RoleUser, Scopes: []string{ScopeDatasetsRead}}
	if !CanAccessOwner(user, ownerID, ScopeDatasetsRead) {
		t.Fatal("owner with scope should be allowed")
	}
	if CanAccessOwner(user, uuid.New(), ScopeDatasetsRead) {
		t.Fatal("non-owner should be denied")
	}
	admin := auth.Principal{UserID: uuid.New(), Role: auth.RoleAdmin}
	if !CanAccessOwner(admin, uuid.New(), ScopeDatasetsWrite) {
		t.Fatal("admin should bypass ownership while retaining known scope policy")
	}
	if err := RequireScope(user, ScopeJobsWrite); err == nil {
		t.Fatal("missing scope should be denied")
	}
}

func TestValidateGrantableScopesRequiresCurrentPermission(t *testing.T) {
	principal := auth.Principal{UserID: uuid.New(), Role: auth.RoleUser, Scopes: []string{ScopeJobsRead}}
	if err := ValidateGrantableScopes(principal, []string{ScopeJobsRead}); err != nil {
		t.Fatalf("expected granted scope to be valid: %v", err)
	}
	if err := ValidateGrantableScopes(principal, []string{ScopeDatasetsWrite}); err == nil {
		t.Fatal("expected ungranted scope to be rejected")
	}
	if err := ValidateGrantableScopes(principal, []string{"unknown:scope"}); err == nil {
		t.Fatal("expected unknown scope to be rejected")
	}
}

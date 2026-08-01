package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
)

func newTestService(t *testing.T, store *memoryStore) *Service {
	t.Helper()
	cfg, err := config.Load(func(key string) string {
		if key == "JWT_SIGNING_KEY" {
			return "test-signing-key-with-at-least-32-bytes"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load returned error: %v", err)
	}
	service, err := NewService(store, cfg)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	now := time.Now().UTC()
	service.Now = func() time.Time { return now }
	service.AccessTokens.Now = func() time.Time { return now }
	return service
}

func TestRegisterLoginAndRefreshReplay(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(t, store)
	ctx := context.Background()
	user, firstPair, err := service.Register(ctx, "Alice@Example.com", "correct horse battery staple", "request-1")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if user.Email != "alice@example.com" || user.Role != auth.RoleUser || firstPair.RefreshToken == "" {
		t.Fatalf("unexpected registration result: %+v", user)
	}
	if store.users[user.ID].PasswordHash == "correct horse battery staple" {
		t.Fatal("plaintext password was stored")
	}
	if _, _, err := service.Login(ctx, "alice@example.com", "wrong password", "request-2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error=%v", err)
	}
	_, secondPair, err := service.Refresh(ctx, firstPair.RefreshToken, "request-3")
	if err != nil || secondPair.RefreshToken == firstPair.RefreshToken {
		t.Fatalf("Refresh returned pair=%+v err=%v", secondPair, err)
	}
	if _, _, err := service.Refresh(ctx, firstPair.RefreshToken, "request-4"); !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("expected refresh replay rejection, got %v", err)
	}
	if !store.familyRevoked[store.refresh[string(auth.HashOpaqueToken(secondPair.RefreshToken))].FamilyID] {
		t.Fatal("refresh replay did not revoke token family")
	}
}

func TestAPIKeyIsScopedAndPlaintextIsReturnedOnlyAtCreation(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(t, store)
	user, _, err := service.Register(context.Background(), "user@example.com", "correct horse battery staple", "request-1")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	principal := auth.Principal{UserID: user.ID, Role: user.Role, Scopes: user.Scopes}
	created, err := service.CreateAPIKey(context.Background(), principal, "automation", []string{authz.ScopeJobsRead}, "request-2")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if created.Plaintext == "" || created.Key.Prefix == created.Plaintext {
		t.Fatalf("unexpected API key disclosure result: %+v", created)
	}
	for _, value := range store.keys {
		if string(value.Hash) == created.Plaintext {
			t.Fatal("plaintext API key was stored")
		}
	}
	apiPrincipal, err := service.AuthenticateAPIKey(context.Background(), created.Plaintext)
	if err != nil || !authz.HasScope(apiPrincipal, authz.ScopeJobsRead) || authz.HasScope(apiPrincipal, authz.ScopeJobsWrite) {
		t.Fatalf("unexpected API key principal=%+v err=%v", apiPrincipal, err)
	}
	if _, err := service.CreateAPIKey(context.Background(), principal, "too-powerful", []string{authz.ScopeJobsWrite, "admin:all"}, "request-3"); !errors.Is(err, authz.ErrInvalidScope) {
		t.Fatalf("expected invalid API key scope, got %v", err)
	}
}

func TestBearerAuthenticationLoadsCurrentPrincipal(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(t, store)
	user, pair, err := service.Register(context.Background(), "bearer@example.com", "correct horse battery staple", "request-1")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	principal, err := service.AuthenticateBearer(context.Background(), pair.AccessToken)
	if err != nil || principal.UserID != user.ID || principal.AuthMethod != "bearer" {
		t.Fatalf("unexpected bearer principal=%+v err=%v", principal, err)
	}
	store.users[user.ID] = User{ID: user.ID, Email: user.Email, Role: user.Role, Scopes: user.Scopes, DisabledAt: ptrTime(time.Now())}
	if _, err := service.AuthenticateBearer(context.Background(), pair.AccessToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user should be rejected, got %v", err)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

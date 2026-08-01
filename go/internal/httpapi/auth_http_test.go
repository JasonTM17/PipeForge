package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/JasonTM17/PipeForge/go/internal/authz"
	"github.com/JasonTM17/PipeForge/go/internal/identity"
	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/google/uuid"
)

type httpMemoryRefresh struct {
	UserID   uuid.UUID
	FamilyID uuid.UUID
	Expires  time.Time
	Used     bool
}

type httpMemoryKey struct {
	Key    identity.APIKey
	UserID uuid.UUID
	Hash   []byte
}

type httpMemoryStore struct {
	users         map[uuid.UUID]identity.User
	byEmail       map[string]uuid.UUID
	refresh       map[string]*httpMemoryRefresh
	familyRevoked map[uuid.UUID]bool
	keys          map[uuid.UUID]httpMemoryKey
}

func newHTTPMemoryStore() *httpMemoryStore {
	return &httpMemoryStore{
		users:         make(map[uuid.UUID]identity.User),
		byEmail:       make(map[string]uuid.UUID),
		refresh:       make(map[string]*httpMemoryRefresh),
		familyRevoked: make(map[uuid.UUID]bool),
		keys:          make(map[uuid.UUID]httpMemoryKey),
	}
}

func (s *httpMemoryStore) CreateUser(_ context.Context, email, passwordHash string, role auth.Role, scopes []string) (identity.User, error) {
	if _, ok := s.byEmail[email]; ok {
		return identity.User{}, identity.ErrEmailAlreadyExists
	}
	user := identity.User{ID: uuid.New(), Email: email, PasswordHash: passwordHash, Role: role, Scopes: append([]string(nil), scopes...)}
	s.users[user.ID] = user
	s.byEmail[email] = user.ID
	return user, nil
}

func (s *httpMemoryStore) FindUserByEmail(_ context.Context, email string) (identity.User, error) {
	userID, ok := s.byEmail[email]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return s.users[userID], nil
}

func (s *httpMemoryStore) FindUserByID(_ context.Context, userID uuid.UUID) (identity.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return user, nil
}

func (s *httpMemoryStore) CreateRefreshSession(_ context.Context, userID, familyID, _ uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	s.refresh[string(tokenHash)] = &httpMemoryRefresh{UserID: userID, FamilyID: familyID, Expires: expiresAt}
	return nil
}

func (s *httpMemoryStore) RotateRefreshToken(_ context.Context, tokenHash []byte, _ uuid.UUID, newHash []byte, expiresAt time.Time) (identity.User, error) {
	entry, ok := s.refresh[string(tokenHash)]
	if !ok || s.familyRevoked[entry.FamilyID] {
		return identity.User{}, identity.ErrRefreshTokenInvalid
	}
	if entry.Used {
		s.familyRevoked[entry.FamilyID] = true
		return identity.User{}, identity.ErrRefreshTokenReplay
	}
	if !entry.Expires.After(time.Now()) {
		return identity.User{}, identity.ErrRefreshTokenExpired
	}
	entry.Used = true
	s.refresh[string(newHash)] = &httpMemoryRefresh{UserID: entry.UserID, FamilyID: entry.FamilyID, Expires: expiresAt}
	return s.users[entry.UserID], nil
}

func (s *httpMemoryStore) RevokeRefreshTokenFamily(_ context.Context, tokenHash []byte) error {
	if entry, ok := s.refresh[string(tokenHash)]; ok {
		s.familyRevoked[entry.FamilyID] = true
	}
	return nil
}

func (s *httpMemoryStore) CreateAPIKey(_ context.Context, userID uuid.UUID, name, prefix string, hash []byte, scopes []string) (identity.APIKey, error) {
	key := identity.APIKey{ID: uuid.New(), Name: name, Prefix: prefix, Scopes: append([]string(nil), scopes...), CreatedAt: time.Now()}
	s.keys[key.ID] = httpMemoryKey{Key: key, UserID: userID, Hash: append([]byte(nil), hash...)}
	return key, nil
}

func (s *httpMemoryStore) ListAPIKeys(_ context.Context, userID uuid.UUID) ([]identity.APIKey, error) {
	keys := make([]identity.APIKey, 0)
	for _, value := range s.keys {
		if value.UserID == userID {
			keys = append(keys, value.Key)
		}
	}
	return keys, nil
}

func (s *httpMemoryStore) RevokeAPIKey(_ context.Context, userID, keyID uuid.UUID) error {
	value, ok := s.keys[keyID]
	if !ok || value.UserID != userID {
		return identity.ErrAPIKeyNotFound
	}
	now := time.Now()
	value.Key.RevokedAt = &now
	s.keys[keyID] = value
	return nil
}

func (s *httpMemoryStore) AuthenticateAPIKey(_ context.Context, value string, hash []byte) (identity.APIKeyAuthentication, error) {
	prefix, ok := auth.APIKeyPrefix(value)
	if !ok {
		return identity.APIKeyAuthentication{}, identity.ErrAPIKeyInvalid
	}
	for _, candidate := range s.keys {
		if candidate.Key.Prefix != prefix || candidate.Key.RevokedAt != nil || subtle.ConstantTimeCompare(candidate.Hash, hash) != 1 {
			continue
		}
		return identity.APIKeyAuthentication{Key: candidate.Key, User: s.users[candidate.UserID], Scopes: candidate.Key.Scopes}, nil
	}
	return identity.APIKeyAuthentication{}, identity.ErrAPIKeyInvalid
}

func (s *httpMemoryStore) RecordAudit(context.Context, identity.AuditEvent) error { return nil }

var _ identity.Store = (*httpMemoryStore)(nil)

func newHTTPIdentityService(t *testing.T, store *httpMemoryStore) *identity.Service {
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
	service, err := identity.NewService(store, cfg)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	return service
}

func TestIdentityHTTPFlowAndCredentialBoundaries(t *testing.T) {
	store := newHTTPMemoryStore()
	router := NewRouter(Dependencies{Identity: newHTTPIdentityService(t, store)})
	registerResponse := performJSONRequest(router, http.MethodPost, "/v1/auth/register", `{"email":"alice@example.com","password":"correct horse battery staple"}`, "")
	if registerResponse.Code != http.StatusCreated || registerResponse.Header().Get(authHeaderRequestID) == "" {
		t.Fatalf("unexpected registration response: %d headers=%v body=%s", registerResponse.Code, registerResponse.Header(), registerResponse.Body.String())
	}
	var tokens tokenResponse
	if err := json.Unmarshal(registerResponse.Body.Bytes(), &tokens); err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("unexpected token response: %v %+v", err, tokens)
	}

	unauthorized := performJSONRequest(router, http.MethodGet, "/v1/api-keys", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated API-key list to fail, got %d", unauthorized.Code)
	}
	created := performJSONRequest(router, http.MethodPost, "/v1/api-keys", `{"name":"worker","scopes":["jobs:read"]}`, tokens.AccessToken)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"apiKey"`) {
		t.Fatalf("unexpected API key creation response: %d %s", created.Code, created.Body.String())
	}
	var createdKey createdAPIKeyResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdKey); err != nil || createdKey.APIKey == "" {
		t.Fatalf("unexpected created API key: %v %+v", err, createdKey)
	}
	listed := performJSONRequest(router, http.MethodGet, "/v1/api-keys", "", tokens.AccessToken)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), `"apiKey"`) {
		t.Fatalf("API key list disclosed plaintext: %d %s", listed.Code, listed.Body.String())
	}

	refreshed := performJSONRequest(router, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"`+tokens.RefreshToken+`"}`, "")
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh failed: %d %s", refreshed.Code, refreshed.Body.String())
	}
	replay := performJSONRequest(router, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"`+tokens.RefreshToken+`"}`, "")
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("refresh replay was not rejected: %d %s", replay.Code, replay.Body.String())
	}

	byAPIKey := performJSONRequestWithAPIKey(router, http.MethodGet, "/v1/api-keys", "", createdKey.APIKey)
	if byAPIKey.Code != http.StatusOK {
		t.Fatalf("API-key authentication failed: %d %s", byAPIKey.Code, byAPIKey.Body.String())
	}
	if err := authz.RequireScope(auth.Principal{UserID: uuid.New(), Role: auth.RoleUser, Scopes: []string{authz.ScopeJobsRead}}, authz.ScopeJobsWrite); err == nil {
		t.Fatal("test must retain deny-by-default scope behavior")
	}
}

const authHeaderRequestID = "X-Request-ID"

func performJSONRequest(router http.Handler, method, path, body, bearer string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func performJSONRequestWithAPIKey(router http.Handler, method, path, body, apiKey string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-API-Key", apiKey)
	router.ServeHTTP(recorder, request)
	return recorder
}

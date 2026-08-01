package identity

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/auth"
	"github.com/google/uuid"
)

type memoryRefresh struct {
	UserID   uuid.UUID
	FamilyID uuid.UUID
	Expires  time.Time
	Used     bool
}

type memoryKey struct {
	Key    APIKey
	UserID uuid.UUID
	Hash   []byte
}

type memoryStore struct {
	users         map[uuid.UUID]User
	byEmail       map[string]uuid.UUID
	refresh       map[string]*memoryRefresh
	familyRevoked map[uuid.UUID]bool
	keys          map[uuid.UUID]memoryKey
	audits        []AuditEvent
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:         make(map[uuid.UUID]User),
		byEmail:       make(map[string]uuid.UUID),
		refresh:       make(map[string]*memoryRefresh),
		familyRevoked: make(map[uuid.UUID]bool),
		keys:          make(map[uuid.UUID]memoryKey),
	}
}

func (s *memoryStore) CreateUser(_ context.Context, email, passwordHash string, role auth.Role, scopes []string) (User, error) {
	if _, exists := s.byEmail[email]; exists {
		return User{}, ErrEmailAlreadyExists
	}
	user := User{ID: uuid.New(), Email: email, PasswordHash: passwordHash, Role: role, Scopes: append([]string(nil), scopes...)}
	s.users[user.ID] = user
	s.byEmail[email] = user.ID
	return user, nil
}

func (s *memoryStore) FindUserByEmail(_ context.Context, email string) (User, error) {
	userID, ok := s.byEmail[email]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return s.users[userID], nil
}

func (s *memoryStore) FindUserByID(_ context.Context, userID uuid.UUID) (User, error) {
	user, ok := s.users[userID]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (s *memoryStore) CreateRefreshSession(_ context.Context, userID, familyID, _ uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	s.refresh[string(tokenHash)] = &memoryRefresh{UserID: userID, FamilyID: familyID, Expires: expiresAt}
	return nil
}

func (s *memoryStore) RotateRefreshToken(_ context.Context, tokenHash []byte, _ uuid.UUID, newTokenHash []byte, expiresAt time.Time) (User, error) {
	entry, ok := s.refresh[string(tokenHash)]
	if !ok || s.familyRevoked[entry.FamilyID] {
		return User{}, ErrRefreshTokenInvalid
	}
	if entry.Used {
		s.familyRevoked[entry.FamilyID] = true
		return User{}, ErrRefreshTokenReplay
	}
	if !entry.Expires.After(time.Now()) {
		return User{}, ErrRefreshTokenExpired
	}
	entry.Used = true
	s.refresh[string(newTokenHash)] = &memoryRefresh{UserID: entry.UserID, FamilyID: entry.FamilyID, Expires: expiresAt}
	return s.users[entry.UserID], nil
}

func (s *memoryStore) RevokeRefreshTokenFamily(_ context.Context, tokenHash []byte) error {
	entry, ok := s.refresh[string(tokenHash)]
	if ok {
		s.familyRevoked[entry.FamilyID] = true
	}
	return nil
}

func (s *memoryStore) CreateAPIKey(_ context.Context, userID uuid.UUID, name, prefix string, keyHash []byte, scopes []string) (APIKey, error) {
	key := APIKey{ID: uuid.New(), Name: name, Prefix: prefix, Scopes: append([]string(nil), scopes...), CreatedAt: time.Now()}
	s.keys[key.ID] = memoryKey{Key: key, UserID: userID, Hash: append([]byte(nil), keyHash...)}
	return key, nil
}

func (s *memoryStore) ListAPIKeys(_ context.Context, userID uuid.UUID) ([]APIKey, error) {
	keys := make([]APIKey, 0)
	for _, value := range s.keys {
		if value.UserID == userID {
			keys = append(keys, value.Key)
		}
	}
	return keys, nil
}

func (s *memoryStore) RevokeAPIKey(_ context.Context, userID, keyID uuid.UUID) error {
	value, ok := s.keys[keyID]
	if !ok || value.UserID != userID {
		return ErrAPIKeyNotFound
	}
	now := time.Now()
	value.Key.RevokedAt = &now
	s.keys[keyID] = value
	return nil
}

func (s *memoryStore) AuthenticateAPIKey(_ context.Context, value string, presentedHash []byte) (APIKeyAuthentication, error) {
	prefix, ok := auth.APIKeyPrefix(value)
	if !ok {
		return APIKeyAuthentication{}, ErrAPIKeyInvalid
	}
	for _, candidate := range s.keys {
		if candidate.Key.Prefix != prefix || candidate.Key.RevokedAt != nil || subtle.ConstantTimeCompare(candidate.Hash, presentedHash) != 1 {
			continue
		}
		user := s.users[candidate.UserID]
		return APIKeyAuthentication{Key: candidate.Key, User: user, Scopes: candidate.Key.Scopes}, nil
	}
	return APIKeyAuthentication{}, ErrAPIKeyInvalid
}

func (s *memoryStore) RecordAudit(_ context.Context, event AuditEvent) error {
	s.audits = append(s.audits, event)
	return nil
}

var _ Store = (*memoryStore)(nil)

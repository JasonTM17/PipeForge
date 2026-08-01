package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessTokenRoundTripAndTamperRejection(t *testing.T) {
	now := time.Now().UTC()
	service := NewAccessTokenService("test-signing-key-with-at-least-32-bytes", time.Minute)
	service.Now = func() time.Time { return now }
	userID := uuid.New()
	serialized, expiresAt, err := service.Issue(userID)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if !expiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected expiration: %s", expiresAt)
	}
	parsed, err := service.Parse(serialized)
	if err != nil || parsed != userID {
		t.Fatalf("Parse returned user=%s err=%v", parsed, err)
	}
	parsed, err = service.Parse(serialized + "tampered")
	if err == nil || parsed != uuid.Nil {
		t.Fatalf("expected tampered token rejection, user=%s err=%v", parsed, err)
	}
}

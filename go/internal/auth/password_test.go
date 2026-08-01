package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hasher := NewPasswordHasher()
	hash, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if !strings.HasPrefix(hash, "argon2id$v=19$") {
		t.Fatalf("unexpected password hash format: %s", hash)
	}
	valid, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !valid {
		t.Fatalf("expected password to verify, valid=%v err=%v", valid, err)
	}
	valid, err = VerifyPassword(hash, "wrong password")
	if err != nil || valid {
		t.Fatalf("expected wrong password to fail, valid=%v err=%v", valid, err)
	}
}

func TestPasswordHashRejectsMalformedStoredValue(t *testing.T) {
	valid, err := VerifyPassword("not-a-password-hash", "correct horse battery staple")
	if err == nil || valid {
		t.Fatalf("expected malformed hash error, valid=%v err=%v", valid, err)
	}
}

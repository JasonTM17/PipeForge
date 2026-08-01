package auth

import "testing"

func TestOpaqueTokenHashIsStableAndNonReversibleByAPI(t *testing.T) {
	token, hash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken returned error: %v", err)
	}
	if token == "" || len(hash) != 32 || string(hash) != string(HashOpaqueToken(token)) {
		t.Fatalf("unexpected opaque token result")
	}
	otherToken, _, err := NewOpaqueToken()
	if err != nil || token == otherToken {
		t.Fatalf("expected unique opaque tokens, other=%q err=%v", otherToken, err)
	}
}

func TestAPIKeyPrefixValidation(t *testing.T) {
	key, prefix, _, err := NewAPIKey()
	if err != nil || prefix == "" {
		t.Fatalf("NewAPIKey returned key=%q prefix=%q err=%v", key, prefix, err)
	}
	parsed, ok := APIKeyPrefix(key)
	if !ok || parsed != prefix {
		t.Fatalf("unexpected API key prefix: %q %v", parsed, ok)
	}
	if _, ok := APIKeyPrefix("wrong"); ok {
		t.Fatal("expected invalid API key prefix to be rejected")
	}
}

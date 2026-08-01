package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	opaqueTokenBytes = 32
	apiKeyPrefix     = "pfk_"
	apiKeyPrefixSize = 12
)

var ErrInvalidOpaqueToken = errors.New("invalid opaque token")

func NewOpaqueToken() (string, []byte, error) {
	value, err := randomURLValue(opaqueTokenBytes)
	if err != nil {
		return "", nil, fmt.Errorf("generate opaque token: %w", err)
	}
	return value, HashOpaqueToken(value), nil
}

func HashOpaqueToken(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func NewAPIKey() (string, string, []byte, error) {
	randomValue, err := randomURLValue(opaqueTokenBytes)
	if err != nil {
		return "", "", nil, fmt.Errorf("generate API key: %w", err)
	}
	value := apiKeyPrefix + randomValue
	prefix, ok := APIKeyPrefix(value)
	if !ok {
		return "", "", nil, ErrInvalidOpaqueToken
	}
	return value, prefix, HashOpaqueToken(value), nil
}

func APIKeyPrefix(value string) (string, bool) {
	if !strings.HasPrefix(value, apiKeyPrefix) || len(value) < apiKeyPrefixSize {
		return "", false
	}
	return value[:apiKeyPrefixSize], true
}

func randomURLValue(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

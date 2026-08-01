package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 2
	passwordSaltLength  = 16
	passwordKeyLength   = 32
	minimumPasswordSize = 12
	maximumPasswordSize = 1024
)

var ErrInvalidPasswordHash = errors.New("invalid password hash")

type PasswordHasher struct {
	Random io.Reader
}

func NewPasswordHasher() PasswordHasher {
	return PasswordHasher{Random: rand.Reader}
}

func (h PasswordHasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	random := h.Random
	if random == nil {
		random = rand.Reader
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := io.ReadFull(random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey(salt, []byte(password), passwordIterations, passwordMemory, passwordParallelism, passwordKeyLength)
	return formatPasswordHash(salt, key), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	if err := ValidatePassword(password); err != nil {
		return false, err
	}
	parameters, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(salt, []byte(password), parameters.iterations, parameters.memory, parameters.parallelism, parameters.keyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("password must be valid UTF-8")
	}
	size := len([]byte(password))
	if size < minimumPasswordSize || size > maximumPasswordSize {
		return fmt.Errorf("password must be between %d and %d bytes", minimumPasswordSize, maximumPasswordSize)
	}
	return nil
}

type passwordParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	keyLength   uint32
}

func formatPasswordHash(salt, key []byte) string {
	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedKey := base64.RawStdEncoding.EncodeToString(key)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemory, passwordIterations, passwordParallelism, encodedSalt, encodedKey)
}

func parsePasswordHash(encoded string) (passwordParameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	values := map[string]uint64{}
	for _, field := range strings.Split(parts[2], ",") {
		key, raw, found := strings.Cut(field, "=")
		if !found || key == "" {
			return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
		}
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
		}
		values[key] = value
	}
	memory, okMemory := values["m"]
	iterations, okIterations := values["t"]
	parallelism, okParallelism := values["p"]
	if !okMemory || !okIterations || !okParallelism || memory < 8*parallelism || memory > 512*1024 || iterations == 0 || iterations > 10 || parallelism == 0 || parallelism > 16 {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return passwordParameters{}, nil, nil, ErrInvalidPasswordHash
	}
	return passwordParameters{
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(parallelism),
		keyLength:   uint32(len(key)),
	}, salt, key, nil
}

package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const AccessTokenIssuer = "pipeforge-api"

var ErrInvalidAccessToken = errors.New("invalid access token")

type AccessTokenService struct {
	SigningKey []byte
	Issuer     string
	TTL        time.Duration
	Now        func() time.Time
}

type accessClaims struct {
	TokenType string `json:"typ"`
	jwt.RegisteredClaims
}

func NewAccessTokenService(signingKey string, ttl time.Duration) AccessTokenService {
	return AccessTokenService{
		SigningKey: []byte(signingKey),
		Issuer:     AccessTokenIssuer,
		TTL:        ttl,
		Now:        time.Now,
	}
}

func (s AccessTokenService) Issue(userID uuid.UUID) (string, time.Time, error) {
	if userID == uuid.Nil || len(s.SigningKey) < 32 || s.TTL <= 0 {
		return "", time.Time{}, ErrInvalidAccessToken
	}
	now := s.now()
	expiresAt := now.Add(s.TTL)
	claims := accessClaims{
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.Issuer,
			Subject:   userID.String(),
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	serialized, err := token.SignedString(s.SigningKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return serialized, expiresAt, nil
}

func (s AccessTokenService) Parse(serialized string) (uuid.UUID, error) {
	if serialized == "" || len(s.SigningKey) < 32 || s.Issuer == "" {
		return uuid.Nil, ErrInvalidAccessToken
	}
	claims := &accessClaims{}
	parser := jwt.NewParser(
		jwt.WithIssuer(s.Issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	token, err := parser.ParseWithClaims(serialized, claims, func(*jwt.Token) (any, error) {
		return s.SigningKey, nil
	})
	if err != nil || token == nil || !token.Valid || claims.TokenType != "access" {
		return uuid.Nil, ErrInvalidAccessToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID == uuid.Nil {
		return uuid.Nil, ErrInvalidAccessToken
	}
	return userID, nil
}

func (s AccessTokenService) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

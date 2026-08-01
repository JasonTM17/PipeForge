package auth

import "github.com/google/uuid"

type Role string

const (
	AuthMethodBearer = "bearer"
	AuthMethodAPIKey = "api-key"
)

const (
	RoleAdmin   Role = "ADMIN"
	RoleUser    Role = "USER"
	RoleService Role = "SERVICE"
)

type Principal struct {
	UserID     uuid.UUID
	Role       Role
	Scopes     []string
	AuthMethod string
}

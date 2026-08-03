package workerregistry

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusStarting  = "STARTING"
	StatusReady     = "READY"
	StatusBusy      = "BUSY"
	StatusDraining  = "DRAINING"
	StatusUnhealthy = "UNHEALTHY"
	StatusOffline   = "OFFLINE"

	DefaultHeartbeatTTL = 45 * time.Second
	MaxHeartbeatTTL     = 30 * time.Minute
	MaxFutureClockSkew  = 5 * time.Minute
)

var (
	ErrInvalidEvent = errors.New("invalid worker event")
)

type Registration struct {
	WorkerID            uuid.UUID
	InstanceID          string
	Hostname            string
	SupportedOperations []string
	SoftwareVersion     string
	MaxConcurrency      int
	StartedAt           time.Time
}

type Heartbeat struct {
	WorkerID            uuid.UUID
	InstanceID          string
	Hostname            string
	SupportedOperations []string
	SoftwareVersion     string
	Status              string
	CurrentConcurrency  int
	MaxConcurrency      int
	CurrentJobIDs       []uuid.UUID
	ObservedAt          time.Time
}

type Clock interface {
	Now() time.Time
}

type Config struct {
	HeartbeatTTL time.Duration
	Clock        Clock
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

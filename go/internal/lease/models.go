package lease

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultLeaseDuration   = 2 * time.Minute
	DefaultRenewalWindow   = 2 * time.Minute
	MaxLeaseDuration       = 30 * time.Minute
	DefaultSweepBatchLimit = 100
	MaxSweepBatchLimit     = 1000
)

var (
	ErrInvalidInput  = errors.New("invalid lease input")
	ErrLeaseNotFound = errors.New("lease not found")
	ErrLeaseExpired  = errors.New("lease has expired")
	ErrLeaseState    = errors.New("lease is not renewable")
	ErrLeaseNotReady = errors.New("job retry is not ready")
)

type Lease struct {
	LeaseID        uuid.UUID `json:"leaseId"`
	JobID          uuid.UUID `json:"jobId"`
	AttemptID      uuid.UUID `json:"attemptId"`
	WorkerID       uuid.UUID `json:"workerId"`
	AttemptNumber  int       `json:"attemptNumber"`
	LeasedAt       time.Time `json:"leasedAt"`
	LeaseExpiresAt time.Time `json:"leaseExpiresAt"`
	LastRenewedAt  time.Time `json:"lastRenewedAt"`
}

type AcquireCommand struct {
	JobID    uuid.UUID
	WorkerID uuid.UUID
	Duration time.Duration
}

type RenewCommand struct {
	LeaseID  uuid.UUID
	WorkerID uuid.UUID
	Duration time.Duration
}

type SweepReport struct {
	Claimed      int
	Retried      int
	DeadLettered int
	Skipped      int
}

type Store interface {
	Acquire(context.Context, AcquireCommand) (Lease, error)
	Renew(context.Context, RenewCommand) (Lease, error)
	SweepExpired(context.Context, int) (SweepReport, error)
}

type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time { return f() }

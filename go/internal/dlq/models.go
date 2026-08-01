package dlq

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const MaxPageSize = 100

var (
	ErrInvalidInput    = errors.New("invalid dead-letter input")
	ErrNotFound        = errors.New("dead-letter record not found")
	ErrAlreadyReplayed = errors.New("dead-letter record was already replayed")
	ErrJobState        = errors.New("job is not eligible for dead-letter replay")
	ErrQuotaExceeded   = errors.New("job quota exceeded")
)

type Record struct {
	ID            uuid.UUID  `json:"id"`
	OwnerUserID   uuid.UUID  `json:"-"`
	JobID         uuid.UUID  `json:"jobId"`
	AttemptID     uuid.UUID  `json:"attemptId"`
	LeaseID       uuid.UUID  `json:"leaseId,omitempty"`
	WorkerID      uuid.UUID  `json:"workerId,omitempty"`
	AttemptNumber int        `json:"attemptNumber"`
	ErrorCode     string     `json:"errorCode"`
	ErrorMessage  string     `json:"errorMessage"`
	Retryable     bool       `json:"retryable"`
	DiagnosticRef *string    `json:"diagnosticRef,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReplayedAt    *time.Time `json:"replayedAt,omitempty"`
	ReplayedBy    *uuid.UUID `json:"replayedBy,omitempty"`
}

type Page struct {
	Items    []Record `json:"items"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
	Total    int64    `json:"total"`
}

type ListQuery struct {
	OwnerUserID *uuid.UUID
	Page        int
	PageSize    int
	OpenOnly    bool
}

type ReplayCommand struct {
	RecordID        uuid.UUID
	ActorUserID     uuid.UUID
	AllowCrossOwner bool
	TraceID         string
	RequestID       string
}

type ReplayResult struct {
	Record    Record    `json:"record"`
	JobID     uuid.UUID `json:"jobId"`
	AttemptID uuid.UUID `json:"attemptId"`
}

type Store interface {
	List(context.Context, ListQuery) (Page, error)
	Get(context.Context, uuid.UUID) (Record, error)
	Replay(context.Context, ReplayCommand) (ReplayResult, error)
}

package job

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StateCreated          = "CREATED"
	StateQueued           = "QUEUED"
	StateLeased           = "LEASED"
	StateRunning          = "RUNNING"
	StateSucceeded        = "SUCCEEDED"
	StateFailedRetryable  = "FAILED_RETRYABLE"
	StateFailedPermanent  = "FAILED_PERMANENT"
	StateCancelRequested  = "CANCEL_REQUESTED"
	StateCancelled        = "CANCELLED"
	StateDeadLettered     = "DEAD_LETTERED"
	AttemptCreated        = "CREATED"
	AttemptLeased         = "LEASED"
	AttemptRunning        = "RUNNING"
	AttemptSucceeded      = "SUCCEEDED"
	AttemptFailed         = "FAILED"
	AttemptTimedOut       = "TIMED_OUT"
	AttemptCancelled      = "CANCELLED"
	DefaultPriority       = 0
	DefaultMaxAttempts    = 5
	MaxPriority           = 100
	MaxAttempts           = 20
	MaxIdempotencyKeySize = 128
)

var (
	ErrInvalidInput        = errors.New("invalid job input")
	ErrJobNotFound         = errors.New("job not found")
	ErrVersionNotFound     = errors.New("dataset version not found")
	ErrJobState            = errors.New("job is not in the required state")
	ErrInvalidTransition   = errors.New("invalid job state transition")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrVersionUnavailable  = errors.New("dataset version is not available for processing")
	ErrQuotaExceeded       = errors.New("job quota exceeded")
)

type Operation struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

type Job struct {
	ID                 uuid.UUID   `json:"id"`
	OwnerUserID        uuid.UUID   `json:"ownerUserId"`
	DatasetVersionID   uuid.UUID   `json:"datasetVersionId"`
	State              string      `json:"state"`
	Operations         []Operation `json:"operations"`
	RequestFingerprint string      `json:"requestFingerprint"`
	Priority           int16       `json:"priority"`
	MaxAttempts        int16       `json:"maxAttempts"`
	CreatedAt          time.Time   `json:"createdAt"`
	UpdatedAt          time.Time   `json:"updatedAt"`
	QueuedAt           *time.Time  `json:"queuedAt,omitempty"`
	NextAttemptAt      *time.Time  `json:"nextAttemptAt,omitempty"`
	StartedAt          *time.Time  `json:"startedAt,omitempty"`
	CancelRequestedAt  *time.Time  `json:"cancelRequestedAt,omitempty"`
	CompletedAt        *time.Time  `json:"completedAt,omitempty"`
	FinishedAt         *time.Time  `json:"finishedAt,omitempty"`
	LastErrorCode      *string     `json:"lastErrorCode,omitempty"`
	LastErrorMessage   *string     `json:"-"`
}

type Attempt struct {
	ID            uuid.UUID  `json:"id"`
	JobID         uuid.UUID  `json:"jobId"`
	AttemptNumber int        `json:"attemptNumber"`
	State         string     `json:"state"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	ErrorCode     *string    `json:"errorCode,omitempty"`
	ErrorMessage  *string    `json:"errorMessage,omitempty"`
	Retryable     bool       `json:"retryable"`
}

type JobPage struct {
	Items    []Job
	Page     int
	PageSize int
	Total    int64
}

type CreateCommand struct {
	OwnerUserID      uuid.UUID
	ActorUserID      uuid.UUID
	DatasetVersionID uuid.UUID
	Operations       []Operation
	IdempotencyKey   string
	Priority         int16
	MaxAttempts      int16
	TraceID          string
	CorrelationID    string
	CausationID      string
}

type CreateRequest struct {
	Operations  []Operation `json:"operations"`
	Priority    int16       `json:"priority,omitempty"`
	MaxAttempts int16       `json:"maxAttempts,omitempty"`
}

type ListQuery struct {
	OwnerUserID *uuid.UUID
	Page        int
	PageSize    int
	State       string
}

type CancelCommand struct {
	JobID           uuid.UUID
	ActorUserID     uuid.UUID
	Reason          string
	TraceID         string
	RequestID       string
	AllowCrossOwner bool
}

type RetryCommand struct {
	JobID           uuid.UUID
	ActorUserID     uuid.UUID
	TraceID         string
	RequestID       string
	AllowCrossOwner bool
}

type QueuedJob struct {
	Job
	AttemptID uuid.UUID
}

type QueueQuery struct {
	Limit int
}

type Store interface {
	Create(context.Context, CreateCommand) (Job, bool, error)
	Get(context.Context, uuid.UUID) (Job, error)
	List(context.Context, ListQuery) (JobPage, error)
	RequestCancel(context.Context, CancelCommand) (Job, error)
	RequestRetry(context.Context, RetryCommand) (Job, error)
}

type QueueStore interface {
	SelectQueued(context.Context, QueueQuery) ([]QueuedJob, error)
}

func (j Job) MarshalOperations() ([]byte, error) {
	return json.Marshal(j.Operations)
}

func ValidateTransition(from, to string) error {
	if !isJobState(from) || !isJobState(to) {
		return ErrInvalidTransition
	}
	allowed := map[string]map[string]struct{}{
		StateCreated: {
			StateQueued: {},
		},
		StateQueued: {
			StateLeased:          {},
			StateCancelRequested: {},
			StateFailedPermanent: {},
		},
		StateLeased: {
			StateQueued:           {},
			StateRunning:         {},
			StateCancelRequested: {},
			StateFailedRetryable: {},
			StateFailedPermanent: {},
			StateDeadLettered:    {},
		},
		StateRunning: {
			StateQueued:           {},
			StateSucceeded:       {},
			StateFailedRetryable: {},
			StateFailedPermanent: {},
			StateCancelRequested: {},
			StateDeadLettered:    {},
		},
		StateFailedRetryable: {
			StateQueued:       {},
			StateDeadLettered: {},
		},
		StateFailedPermanent: {
			StateQueued: {},
		},
		StateCancelRequested: {
			StateCancelled: {},
		},
		StateDeadLettered: {
			StateQueued: {},
		},
	}
	if _, ok := allowed[from][to]; !ok {
		return ErrInvalidTransition
	}
	return nil
}

func IsTerminal(state string) bool {
	return state == StateSucceeded || state == StateCancelled
}

func IsKnownState(state string) bool {
	return isJobState(state)
}

func isJobState(state string) bool {
	switch state {
	case StateCreated, StateQueued, StateLeased, StateRunning, StateSucceeded,
		StateFailedRetryable, StateFailedPermanent, StateCancelRequested,
		StateCancelled, StateDeadLettered:
		return true
	default:
		return false
	}
}

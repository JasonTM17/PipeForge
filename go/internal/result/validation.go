package result

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

var (
	artifactKeyPattern  = regexp.MustCompile(`^[a-z0-9/_-]+\.[a-z0-9]+$`)
	artifactKindPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	stagePattern        = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	checksumPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type StartedEvent struct {
	JobID         uuid.UUID `json:"jobId"`
	AttemptID     uuid.UUID `json:"attemptId"`
	LeaseID       uuid.UUID `json:"leaseId"`
	WorkerID      uuid.UUID `json:"workerId"`
	AttemptNumber int       `json:"attemptNumber"`
}

type ProgressedEvent struct {
	JobID              uuid.UUID `json:"jobId"`
	AttemptID          uuid.UUID `json:"attemptId"`
	LeaseID            uuid.UUID `json:"leaseId"`
	Stage              string    `json:"stage"`
	ProcessedRows      int64     `json:"processedRows"`
	EstimatedTotalRows *int64    `json:"estimatedTotalRows,omitempty"`
	ProgressPercent    float64   `json:"progressPercent"`
	Throughput         float64   `json:"throughput,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type SucceededEvent struct {
	JobID     uuid.UUID `json:"jobId"`
	AttemptID uuid.UUID `json:"attemptId"`
	LeaseID   uuid.UUID `json:"leaseId"`
	WorkerID  uuid.UUID `json:"workerId"`
	Artifacts []string  `json:"artifacts"`
}

type FailedEvent struct {
	JobID     uuid.UUID   `json:"jobId"`
	AttemptID uuid.UUID   `json:"attemptId"`
	LeaseID   uuid.UUID   `json:"leaseId"`
	WorkerID  uuid.UUID   `json:"workerId"`
	Error     FailureInfo `json:"error"`
}

type CancelledEvent struct {
	JobID     uuid.UUID `json:"jobId"`
	AttemptID uuid.UUID `json:"attemptId"`
	LeaseID   uuid.UUID `json:"leaseId"`
	WorkerID  uuid.UUID `json:"workerId"`
	Reason    string    `json:"reason"`
}

type FailureInfo struct {
	Code          string  `json:"code"`
	Message       string  `json:"message"`
	Retryable     bool    `json:"retryable"`
	DiagnosticRef *string `json:"diagnosticRef,omitempty"`
}

type ArtifactCreatedEvent struct {
	JobID          uuid.UUID `json:"jobId"`
	AttemptID      uuid.UUID `json:"attemptId"`
	LeaseID        uuid.UUID `json:"leaseId"`
	ArtifactID     uuid.UUID `json:"artifactId"`
	Kind           string    `json:"kind"`
	ObjectKey      string    `json:"objectKey"`
	SizeBytes      int64     `json:"sizeBytes"`
	ContentType    string    `json:"contentType"`
	ChecksumSHA256 string    `json:"checksumSha256"`
}

func DecodeEvent(envelope queue.Envelope) (any, error) {
	if err := envelope.Validate(); err != nil {
		return nil, fmt.Errorf("%w: envelope: %v", ErrInvalidEvent, err)
	}
	var event any
	switch envelope.MessageType {
	case queue.MessageJobStarted:
		event = &StartedEvent{}
	case queue.MessageJobProgressed:
		event = &ProgressedEvent{}
	case queue.MessageJobSucceeded:
		event = &SucceededEvent{}
	case queue.MessageJobFailed:
		event = &FailedEvent{}
	case queue.MessageJobCancelled:
		event = &CancelledEvent{}
	case queue.MessageArtifactCreated:
		event = &ArtifactCreatedEvent{}
	default:
		return nil, fmt.Errorf("%w: message type %s is not a result event", ErrInvalidEvent, envelope.MessageType)
	}
	decoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(event); err != nil {
		return nil, fmt.Errorf("%w: payload: %v", ErrInvalidEvent, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: payload must contain one JSON value", ErrInvalidEvent)
	}
	if err := validateEvent(event); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	return event, nil
}

func validateEvent(event any) error {
	switch typed := event.(type) {
	case *StartedEvent:
		if err := validateAttemptIdentity(typed.JobID, typed.AttemptID, typed.LeaseID, typed.WorkerID); err != nil {
			return err
		}
		if typed.WorkerID == uuid.Nil {
			return fmt.Errorf("workerId is required")
		}
		if typed.AttemptNumber < 1 {
			return fmt.Errorf("attemptNumber must be positive")
		}
	case *ProgressedEvent:
		if err := validateAttemptIdentity(typed.JobID, typed.AttemptID, typed.LeaseID, uuid.Nil); err != nil {
			return err
		}
		if !stagePattern.MatchString(typed.Stage) || typed.ProcessedRows < 0 || typed.ProgressPercent < 0 || typed.ProgressPercent > 100 || typed.Throughput < 0 || typed.UpdatedAt.IsZero() {
			return fmt.Errorf("progress fields are invalid")
		}
		if typed.EstimatedTotalRows != nil && *typed.EstimatedTotalRows < 0 {
			return fmt.Errorf("estimatedTotalRows must not be negative")
		}
		if typed.EstimatedTotalRows != nil && *typed.EstimatedTotalRows < typed.ProcessedRows {
			return fmt.Errorf("estimatedTotalRows must not be less than processedRows")
		}
	case *SucceededEvent:
		if err := validateAttemptIdentity(typed.JobID, typed.AttemptID, typed.LeaseID, typed.WorkerID); err != nil {
			return err
		}
		if typed.WorkerID == uuid.Nil {
			return fmt.Errorf("workerId is required")
		}
		if len(typed.Artifacts) > MaxArtifactsPerResult {
			return fmt.Errorf("artifacts must contain at most %d items", MaxArtifactsPerResult)
		}
		if err := validateObjectKeys(typed.Artifacts); err != nil {
			return err
		}
	case *FailedEvent:
		if err := validateAttemptIdentity(typed.JobID, typed.AttemptID, typed.LeaseID, typed.WorkerID); err != nil {
			return err
		}
		if typed.WorkerID == uuid.Nil {
			return fmt.Errorf("workerId is required")
		}
		if strings.TrimSpace(typed.Error.Code) == "" || len(typed.Error.Code) > 128 || len(typed.Error.Message) == 0 || len(typed.Error.Message) > 500 {
			return fmt.Errorf("failure details are invalid")
		}
		if typed.Error.DiagnosticRef != nil && len(*typed.Error.DiagnosticRef) > 512 {
			return fmt.Errorf("diagnosticRef is too long")
		}
	case *CancelledEvent:
		if err := validateAttemptIdentity(typed.JobID, typed.AttemptID, typed.LeaseID, typed.WorkerID); err != nil {
			return err
		}
		if typed.WorkerID == uuid.Nil || strings.TrimSpace(typed.Reason) == "" || len(typed.Reason) > 500 {
			return fmt.Errorf("cancellation fields are invalid")
		}
	case *ArtifactCreatedEvent:
		if err := validateAttemptIdentity(typed.JobID, typed.AttemptID, typed.LeaseID, uuid.Nil); err != nil {
			return err
		}
		if typed.ArtifactID == uuid.Nil || !artifactKindPattern.MatchString(typed.Kind) || !artifactKeyPattern.MatchString(typed.ObjectKey) || typed.SizeBytes < 0 || strings.TrimSpace(typed.ContentType) == "" || len(typed.ContentType) > 128 || !checksumPattern.MatchString(typed.ChecksumSHA256) {
			return fmt.Errorf("artifact fields are invalid")
		}
	default:
		return fmt.Errorf("unsupported result event")
	}
	return nil
}

func validateAttemptIdentity(jobID, attemptID, leaseID, workerID uuid.UUID) error {
	if jobID == uuid.Nil || attemptID == uuid.Nil || leaseID == uuid.Nil {
		return fmt.Errorf("jobId, attemptId, and leaseId are required")
	}
	return nil
}

func validateObjectKeys(keys []string) error {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if !artifactKeyPattern.MatchString(key) || len(key) > 512 {
			return fmt.Errorf("artifact object key is invalid")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("artifact object keys must be unique")
		}
		seen[key] = struct{}{}
	}
	return nil
}

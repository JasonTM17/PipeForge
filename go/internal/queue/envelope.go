package queue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
)

const (
	MaxEnvelopeBytes = 1 << 20

	MessageJobRequested     = "processing.job.requested"
	MessageJobCancel        = "processing.job.cancel-requested"
	MessageJobStarted       = "processing.job.started"
	MessageJobProgressed    = "processing.job.progressed"
	MessageJobSucceeded     = "processing.job.succeeded"
	MessageJobFailed        = "processing.job.failed"
	MessageWorkerHeartbeat  = "processing.worker.heartbeat"
	MessageWorkerRegistered = "processing.worker.registered"
	MessageArtifactCreated  = "processing.artifact.created"
)

var (
	errEmptyMessageID  = errors.New("message ID is required")
	messageTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9-]+)+$`)
	knownMessageTypes  = map[string][]string{
		MessageJobRequested:     {"jobId", "datasetVersionId", "operations"},
		MessageJobCancel:        {"jobId", "reason"},
		MessageJobStarted:       {"jobId", "attemptId", "leaseId", "workerId", "attemptNumber"},
		MessageJobProgressed:    {"jobId", "attemptId", "leaseId", "stage", "processedRows", "progressPercent", "updatedAt"},
		MessageJobSucceeded:     {"jobId", "attemptId", "leaseId", "workerId", "artifacts"},
		MessageJobFailed:        {"jobId", "attemptId", "leaseId", "workerId", "error"},
		MessageWorkerHeartbeat:  {"workerId", "instanceId", "hostname", "supportedOperations", "softwareVersion", "status", "currentConcurrency", "maxConcurrency", "currentJobIds", "observedAt"},
		MessageWorkerRegistered: {"workerId", "instanceId", "hostname", "supportedOperations", "softwareVersion", "maxConcurrency", "startedAt"},
		MessageArtifactCreated:  {"jobId", "attemptId", "leaseId", "artifactId", "kind", "objectKey", "sizeBytes", "contentType", "checksumSha256"},
	}
)

type Envelope struct {
	MessageID     uuid.UUID       `json:"messageId"`
	MessageType   string          `json:"messageType"`
	SchemaVersion int             `json:"schemaVersion"`
	OccurredAt    time.Time       `json:"occurredAt"`
	TraceID       string          `json:"traceId"`
	CorrelationID string          `json:"correlationId"`
	CausationID   string          `json:"causationId"`
	Payload       json.RawMessage `json:"payload"`
}

func NewEnvelope(messageType string, traceID, correlationID, causationID string, payload any) (Envelope, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal %s payload: %w", messageType, err)
	}
	envelope := Envelope{
		MessageID: uuid.New(), MessageType: messageType, SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), TraceID: traceID, CorrelationID: correlationID,
		CausationID: causationID, Payload: encoded,
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func (e Envelope) Validate() error {
	if e.MessageID == uuid.Nil {
		return errEmptyMessageID
	}
	if !messageTypePattern.MatchString(e.MessageType) {
		return fmt.Errorf("message type %q is invalid", e.MessageType)
	}
	if _, ok := knownMessageTypes[e.MessageType]; !ok {
		return fmt.Errorf("message type %q is not registered", e.MessageType)
	}
	if e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema version %d", e.SchemaVersion)
	}
	if e.OccurredAt.IsZero() {
		return errors.New("occurredAt is required")
	}
	if e.TraceID == "" || e.CorrelationID == "" || e.CausationID == "" {
		return errors.New("traceId, correlationId, and causationId are required")
	}
	if len(e.TraceID) > 128 || len(e.CorrelationID) > 128 || len(e.CausationID) > 128 {
		return errors.New("trace context identifiers are too long")
	}
	trimmedPayload := bytes.TrimSpace(e.Payload)
	if len(trimmedPayload) == 0 || !json.Valid(trimmedPayload) || trimmedPayload[0] != '{' {
		return errors.New("payload must be a JSON object")
	}
	if len(trimmedPayload) > MaxEnvelopeBytes {
		return fmt.Errorf("payload exceeds %d bytes", MaxEnvelopeBytes)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(trimmedPayload, &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	for _, field := range knownMessageTypes[e.MessageType] {
		value, ok := payload[field]
		if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("payload.%s is required for %s", field, e.MessageType)
		}
	}
	if e.MessageType == MessageJobRequested {
		var operations []json.RawMessage
		if err := json.Unmarshal(payload["operations"], &operations); err != nil || len(operations) == 0 {
			return errors.New("payload.operations must contain at least one operation")
		}
	}
	return nil
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	type plain Envelope
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(plain(e))
}

func MarshalEnvelope(e Envelope) ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	type plain Envelope
	return json.Marshal(plain(e))
}

func UnmarshalEnvelope(data []byte) (Envelope, error) {
	if len(data) > MaxEnvelopeBytes {
		return Envelope{}, fmt.Errorf("message exceeds %d bytes", MaxEnvelopeBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode message envelope: %w", err)
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func MessageTypes() []string {
	types := make([]string, 0, len(knownMessageTypes))
	for messageType := range knownMessageTypes {
		types = append(types, messageType)
	}
	sort.Strings(types)
	return types
}

package workerregistry

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

var operationPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

type registrationPayload struct {
	WorkerID            uuid.UUID `json:"workerId"`
	InstanceID          string    `json:"instanceId"`
	Hostname            string    `json:"hostname"`
	SupportedOperations []string  `json:"supportedOperations"`
	SoftwareVersion     string    `json:"softwareVersion"`
	MaxConcurrency      int       `json:"maxConcurrency"`
	StartedAt           time.Time `json:"startedAt"`
}

type heartbeatPayload struct {
	WorkerID            uuid.UUID   `json:"workerId"`
	InstanceID          string      `json:"instanceId"`
	Hostname            string      `json:"hostname"`
	SupportedOperations []string    `json:"supportedOperations"`
	SoftwareVersion     string      `json:"softwareVersion"`
	Status              string      `json:"status"`
	CurrentConcurrency  int         `json:"currentConcurrency"`
	MaxConcurrency      int         `json:"maxConcurrency"`
	CurrentJobIDs       []uuid.UUID `json:"currentJobIds"`
	ObservedAt          time.Time   `json:"observedAt"`
}

func ParseRegistration(envelope queue.Envelope) (Registration, error) {
	if envelope.MessageType != queue.MessageWorkerRegistered {
		return Registration{}, fmt.Errorf("%w: unexpected message type %q", ErrInvalidEvent, envelope.MessageType)
	}
	var payload registrationPayload
	if err := decodeStrict(envelope.Payload, &payload); err != nil {
		return Registration{}, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if err := validateIdentity(payload.WorkerID, payload.InstanceID, payload.Hostname, payload.SoftwareVersion, payload.SupportedOperations, payload.MaxConcurrency); err != nil {
		return Registration{}, err
	}
	if payload.StartedAt.IsZero() {
		return Registration{}, fmt.Errorf("%w: startedAt is required", ErrInvalidEvent)
	}
	return Registration{
		WorkerID: payload.WorkerID, InstanceID: strings.TrimSpace(payload.InstanceID), Hostname: strings.TrimSpace(payload.Hostname),
		SupportedOperations: normalizeOperations(payload.SupportedOperations), SoftwareVersion: strings.TrimSpace(payload.SoftwareVersion),
		MaxConcurrency: payload.MaxConcurrency, StartedAt: payload.StartedAt.UTC(),
	}, nil
}

func ParseHeartbeat(envelope queue.Envelope) (Heartbeat, error) {
	if envelope.MessageType != queue.MessageWorkerHeartbeat {
		return Heartbeat{}, fmt.Errorf("%w: unexpected message type %q", ErrInvalidEvent, envelope.MessageType)
	}
	var payload heartbeatPayload
	if err := decodeStrict(envelope.Payload, &payload); err != nil {
		return Heartbeat{}, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if err := validateIdentity(payload.WorkerID, payload.InstanceID, payload.Hostname, payload.SoftwareVersion, payload.SupportedOperations, payload.MaxConcurrency); err != nil {
		return Heartbeat{}, err
	}
	if !isHeartbeatStatus(payload.Status) {
		return Heartbeat{}, fmt.Errorf("%w: status is invalid", ErrInvalidEvent)
	}
	if payload.CurrentConcurrency < 0 || payload.CurrentConcurrency > payload.MaxConcurrency {
		return Heartbeat{}, fmt.Errorf("%w: current concurrency is out of bounds", ErrInvalidEvent)
	}
	if len(payload.CurrentJobIDs) > 128 || payload.ObservedAt.IsZero() {
		return Heartbeat{}, fmt.Errorf("%w: heartbeat jobs or observedAt are invalid", ErrInvalidEvent)
	}
	seenJobs := make(map[uuid.UUID]struct{}, len(payload.CurrentJobIDs))
	for _, jobID := range payload.CurrentJobIDs {
		if jobID == uuid.Nil {
			return Heartbeat{}, fmt.Errorf("%w: current job IDs must be non-zero", ErrInvalidEvent)
		}
		seenJobs[jobID] = struct{}{}
	}
	jobIDs := make([]uuid.UUID, 0, len(seenJobs))
	for jobID := range seenJobs {
		jobIDs = append(jobIDs, jobID)
	}
	return Heartbeat{
		WorkerID: payload.WorkerID, InstanceID: strings.TrimSpace(payload.InstanceID), Hostname: strings.TrimSpace(payload.Hostname),
		SupportedOperations: normalizeOperations(payload.SupportedOperations), SoftwareVersion: strings.TrimSpace(payload.SoftwareVersion),
		Status: payload.Status, CurrentConcurrency: payload.CurrentConcurrency, MaxConcurrency: payload.MaxConcurrency,
		CurrentJobIDs: jobIDs, ObservedAt: payload.ObservedAt.UTC(),
	}, nil
}

func decodeStrict(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("payload must contain exactly one JSON object")
	}
	return nil
}

func validateIdentity(workerID uuid.UUID, instanceID, hostname, version string, operations []string, maxConcurrency int) error {
	if workerID == uuid.Nil {
		return fmt.Errorf("%w: workerId must be non-zero", ErrInvalidEvent)
	}
	if !boundedText(instanceID, 128) || !boundedText(hostname, 255) || !boundedText(version, 64) {
		return fmt.Errorf("%w: worker identity text is invalid", ErrInvalidEvent)
	}
	if len(operations) < 1 || len(operations) > 64 || maxConcurrency < 1 || maxConcurrency > 128 {
		return fmt.Errorf("%w: operations or max concurrency are out of bounds", ErrInvalidEvent)
	}
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		normalized := strings.TrimSpace(operation)
		if !operationPattern.MatchString(normalized) {
			return fmt.Errorf("%w: supported operation is invalid", ErrInvalidEvent)
		}
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf("%w: supported operations must be unique", ErrInvalidEvent)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func boundedText(value string, maximum int) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maximum {
		return false
	}
	for _, character := range trimmed {
		if character < 32 || character == 127 {
			return false
		}
	}
	return true
}

func normalizeOperations(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimSpace(value)
	}
	return result
}

func isHeartbeatStatus(status string) bool {
	switch status {
	case StatusStarting, StatusReady, StatusBusy, StatusDraining, StatusUnhealthy, StatusOffline:
		return true
	default:
		return false
	}
}

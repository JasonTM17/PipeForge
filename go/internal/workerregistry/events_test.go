package workerregistry

import (
	"errors"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

func TestParseRegistrationAndHeartbeatValidateWorkerIdentity(t *testing.T) {
	workerID := uuid.New()
	startedAt := time.Date(2026, 8, 3, 7, 0, 0, 0, time.UTC)
	registrationEnvelope, err := queue.NewEnvelope(queue.MessageWorkerRegistered, "trace", workerID.String(), "start", map[string]any{
		"workerId": workerID, "instanceId": "instance-1", "hostname": "worker-a",
		"supportedOperations": []string{"PROFILE_DATASET", "DETECT_OUTLIERS"},
		"softwareVersion":     "0.1.0", "maxConcurrency": 2, "startedAt": startedAt,
	})
	if err != nil {
		t.Fatalf("NewEnvelope registration: %v", err)
	}
	registration, err := ParseRegistration(registrationEnvelope)
	if err != nil {
		t.Fatalf("ParseRegistration returned error: %v", err)
	}
	if registration.WorkerID != workerID || registration.InstanceID != "instance-1" || registration.MaxConcurrency != 2 {
		t.Fatalf("unexpected registration: %+v", registration)
	}

	heartbeatEnvelope, err := queue.NewEnvelope(queue.MessageWorkerHeartbeat, "trace", workerID.String(), "heartbeat", map[string]any{
		"workerId": workerID, "instanceId": "instance-1", "hostname": "worker-a",
		"supportedOperations": []string{"PROFILE_DATASET", "DETECT_OUTLIERS"},
		"softwareVersion":     "0.1.0", "status": StatusReady, "currentConcurrency": 1,
		"maxConcurrency": 2, "currentJobIds": []uuid.UUID{uuid.New()}, "observedAt": startedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("NewEnvelope heartbeat: %v", err)
	}
	heartbeat, err := ParseHeartbeat(heartbeatEnvelope)
	if err != nil {
		t.Fatalf("ParseHeartbeat returned error: %v", err)
	}
	if heartbeat.Status != StatusReady || heartbeat.CurrentConcurrency != 1 || len(heartbeat.CurrentJobIDs) != 1 {
		t.Fatalf("unexpected heartbeat: %+v", heartbeat)
	}
}

func TestParseHeartbeatRejectsUnknownFieldsAndInvalidCapacity(t *testing.T) {
	workerID := uuid.New()
	envelope, err := queue.NewEnvelope(queue.MessageWorkerHeartbeat, "trace", workerID.String(), "heartbeat", map[string]any{
		"workerId": workerID, "instanceId": "instance-1", "hostname": "worker-a",
		"supportedOperations": []string{"PROFILE_DATASET"}, "softwareVersion": "0.1.0",
		"status": StatusReady, "currentConcurrency": 2, "maxConcurrency": 1,
		"currentJobIds": []uuid.UUID{}, "observedAt": time.Now().UTC(), "unexpected": true,
	})
	if err != nil {
		t.Fatalf("NewEnvelope heartbeat: %v", err)
	}
	_, err = ParseHeartbeat(envelope)
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("ParseHeartbeat error=%v, want ErrInvalidEvent", err)
	}
}

func TestNormalizedRequiredOperationsDeduplicatesAndSorts(t *testing.T) {
	operations, err := normalizedRequiredOperations([]string{"PROFILE_DATASET", "DETECT_OUTLIERS", "PROFILE_DATASET"})
	if err != nil {
		t.Fatalf("normalizedRequiredOperations returned error: %v", err)
	}
	if len(operations) != 2 || operations[0] != "DETECT_OUTLIERS" || operations[1] != "PROFILE_DATASET" {
		t.Fatalf("unexpected operations: %v", operations)
	}
	if _, err := normalizedRequiredOperations([]string{"invalid-operation"}); err == nil {
		t.Fatal("invalid required operation was accepted")
	}
}

func TestValidateEventTimestampRejectsFutureClockPoisoning(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if err := validateEventTimestamp(now, now.Add(MaxFutureClockSkew), "observedAt"); err != nil {
		t.Fatalf("timestamp at skew boundary was rejected: %v", err)
	}
	if err := validateEventTimestamp(now, now.Add(MaxFutureClockSkew+time.Nanosecond), "observedAt"); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("future timestamp error=%v, want ErrInvalidEvent", err)
	}
}

package result

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

func TestDecodeEventAcceptsSupportedResultEvents(t *testing.T) {
	ids := map[string]uuid.UUID{"job": uuid.New(), "attempt": uuid.New(), "lease": uuid.New(), "worker": uuid.New(), "artifact": uuid.New()}
	cases := []struct {
		name    string
		kind    string
		payload any
	}{
		{name: "started", kind: queue.MessageJobStarted, payload: StartedEvent{JobID: ids["job"], AttemptID: ids["attempt"], LeaseID: ids["lease"], WorkerID: ids["worker"], AttemptNumber: 1}},
		{name: "progressed", kind: queue.MessageJobProgressed, payload: ProgressedEvent{JobID: ids["job"], AttemptID: ids["attempt"], LeaseID: ids["lease"], Stage: "PROFILE", ProcessedRows: 10, ProgressPercent: 50, UpdatedAt: time.Now().UTC()}},
		{name: "succeeded", kind: queue.MessageJobSucceeded, payload: SucceededEvent{JobID: ids["job"], AttemptID: ids["attempt"], LeaseID: ids["lease"], WorkerID: ids["worker"], Artifacts: []string{"reports/profile.json"}}},
		{name: "failed", kind: queue.MessageJobFailed, payload: FailedEvent{JobID: ids["job"], AttemptID: ids["attempt"], LeaseID: ids["lease"], WorkerID: ids["worker"], Error: FailureInfo{Code: "INPUT_INVALID", Message: "invalid input", Retryable: false}}},
		{name: "cancelled", kind: queue.MessageJobCancelled, payload: CancelledEvent{JobID: ids["job"], AttemptID: ids["attempt"], LeaseID: ids["lease"], WorkerID: ids["worker"], Reason: "owner_requested"}},
		{name: "artifact", kind: queue.MessageArtifactCreated, payload: ArtifactCreatedEvent{JobID: ids["job"], AttemptID: ids["attempt"], LeaseID: ids["lease"], ArtifactID: ids["artifact"], Kind: "profile", ObjectKey: "reports/profile.json", SizeBytes: 10, ContentType: "application/json", ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			envelope, err := queue.NewEnvelope(testCase.kind, "trace", "correlation", "causation", testCase.payload)
			if err != nil {
				t.Fatalf("NewEnvelope returned error: %v", err)
			}
			if _, err := DecodeEvent(envelope); err != nil {
				t.Fatalf("DecodeEvent returned error: %v", err)
			}
		})
	}
}

func TestDecodeEventRejectsUnknownPayloadAndUnsafeArtifactKey(t *testing.T) {
	jobID, attemptID, leaseID, workerID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	unknownPayload := map[string]any{
		"jobId": jobID, "attemptId": attemptID, "leaseId": leaseID, "workerId": workerID,
		"attemptNumber": 1, "unexpected": true,
	}
	envelope, err := queue.NewEnvelope(queue.MessageJobStarted, "trace", "correlation", "causation", unknownPayload)
	if err != nil {
		t.Fatalf("NewEnvelope returned error: %v", err)
	}
	if _, err := DecodeEvent(envelope); err == nil {
		t.Fatal("unknown payload field was accepted")
	}

	unsafe := ArtifactCreatedEvent{JobID: jobID, AttemptID: attemptID, LeaseID: leaseID, ArtifactID: uuid.New(), Kind: "profile", ObjectKey: "../secret.json", SizeBytes: 1, ContentType: "application/json", ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	envelope, err = queue.NewEnvelope(queue.MessageArtifactCreated, "trace", "correlation", "causation", unsafe)
	if err != nil {
		t.Fatalf("NewEnvelope returned error: %v", err)
	}
	if _, err := DecodeEvent(envelope); err == nil {
		t.Fatal("unsafe artifact key was accepted")
	}
}

func TestDecodeEventRejectsTrailingJSONValue(t *testing.T) {
	payload, err := json.Marshal(StartedEvent{JobID: uuid.New(), AttemptID: uuid.New(), LeaseID: uuid.New(), WorkerID: uuid.New(), AttemptNumber: 1})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	envelope := queue.Envelope{MessageID: uuid.New(), MessageType: queue.MessageJobStarted, SchemaVersion: 1, OccurredAt: time.Now().UTC(), TraceID: "trace", CorrelationID: "correlation", CausationID: "causation", Payload: append(payload, []byte(" ")...)}
	if _, err := DecodeEvent(envelope); err != nil {
		t.Fatalf("whitespace after payload should be accepted: %v", err)
	}
}

func TestDecodeEventRejectsProgressWithImpossibleEstimate(t *testing.T) {
	estimate := int64(10)
	envelope, err := queue.NewEnvelope(queue.MessageJobProgressed, "trace", "correlation", "causation", ProgressedEvent{
		JobID: uuid.New(), AttemptID: uuid.New(), LeaseID: uuid.New(), Stage: "PROCESS",
		ProcessedRows: 11, EstimatedTotalRows: &estimate, ProgressPercent: 100, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewEnvelope rejected an event before result validation: %v", err)
	}
	if _, err := DecodeEvent(envelope); err == nil {
		t.Fatal("DecodeEvent accepted an impossible progress estimate")
	}
}

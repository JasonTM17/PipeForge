package queue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEnvelopeRoundTripAndValidation(t *testing.T) {
	envelope, err := NewEnvelope(MessageJobRequested, "trace-1", "correlation-1", "causation-1", validJobRequestedPayload())
	if err != nil {
		t.Fatalf("NewEnvelope returned error: %v", err)
	}
	encoded, err := MarshalEnvelope(envelope)
	if err != nil {
		t.Fatalf("MarshalEnvelope returned error: %v", err)
	}
	decoded, err := UnmarshalEnvelope(encoded)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope returned error: %v", err)
	}
	if decoded.MessageID != envelope.MessageID || decoded.MessageType != MessageJobRequested {
		t.Fatalf("round trip changed envelope: %+v", decoded)
	}
}

func TestJobQueuedEnvelopeIsJobOnlySchedulerSignal(t *testing.T) {
	envelope, err := NewEnvelope(MessageJobQueued, "trace-1", "correlation-1", "causation-1", map[string]any{
		"jobId": "22222222-2222-4222-8222-222222222222",
	})
	if err != nil {
		t.Fatalf("NewEnvelope returned error: %v", err)
	}
	if envelope.SchemaVersion != 1 {
		t.Fatalf("queued signal changed envelope version: %d", envelope.SchemaVersion)
	}
}

func TestJobRequestedEnvelopeRequiresLeaseBoundFields(t *testing.T) {
	for _, field := range []string{"attemptId", "leaseId", "workerId", "attemptNumber", "operations", "source"} {
		t.Run("missing "+field, func(t *testing.T) {
			payload := validJobRequestedPayload()
			delete(payload, field)
			if _, err := NewEnvelope(MessageJobRequested, "trace-1", "correlation-1", "causation-1", payload); err == nil {
				t.Fatalf("missing %s was accepted", field)
			}
		})
	}
}

func TestEnvelopeRejectsUnknownOrMalformedMessages(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Envelope)
	}{
		{name: "unknown type", mutate: func(e *Envelope) { e.MessageType = "processing.unknown" }},
		{name: "missing trace", mutate: func(e *Envelope) { e.TraceID = "" }},
		{name: "empty operations", mutate: func(e *Envelope) {
			e.Payload = json.RawMessage(`{"jobId":"22222222-2222-4222-8222-222222222222","datasetVersionId":"33333333-3333-4333-8333-333333333333","attemptId":"44444444-4444-4444-8444-444444444444","leaseId":"55555555-5555-4555-8555-555555555555","workerId":"66666666-6666-4666-8666-666666666666","attemptNumber":1,"operations":[],"source":{"objectKey":"datasets/22222222-2222-4222-8222-222222222222/versions/33333333-3333-4333-8333-333333333333/raw","contentType":"text/csv","format":"CSV","sizeBytes":2048}}`)
		}},
		{name: "nil message", mutate: func(e *Envelope) { e.MessageID = uuid.Nil }},
	}
	base, err := NewEnvelope(MessageJobRequested, "trace-1", "correlation-1", "causation-1", validJobRequestedPayload())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func validJobRequestedPayload() map[string]any {
	return map[string]any{
		"jobId":            "22222222-2222-4222-8222-222222222222",
		"datasetVersionId": "33333333-3333-4333-8333-333333333333",
		"attemptId":        "44444444-4444-4444-8444-444444444444",
		"leaseId":          "55555555-5555-4555-8555-555555555555",
		"workerId":         "66666666-6666-4666-8666-666666666666",
		"attemptNumber":    1,
		"operations":       []any{map[string]any{"type": "PROFILE_DATASET", "config": map[string]any{}}},
		"source": map[string]any{
			"objectKey":   "datasets/22222222-2222-4222-8222-222222222222/versions/33333333-3333-4333-8333-333333333333/raw",
			"contentType": "text/csv",
			"format":      "CSV",
			"sizeBytes":   2048,
		},
	}
}

func TestUnmarshalEnvelopeRejectsUnknownFields(t *testing.T) {
	data := []byte(`{"messageId":"11111111-1111-4111-8111-111111111111","messageType":"processing.job.cancel-requested","schemaVersion":1,"occurredAt":"2026-08-01T12:00:00Z","traceId":"trace","correlationId":"correlation","causationId":"causation","payload":{"jobId":"22222222-2222-4222-8222-222222222222","reason":"user request"},"unexpected":true}`)
	if _, err := UnmarshalEnvelope(data); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

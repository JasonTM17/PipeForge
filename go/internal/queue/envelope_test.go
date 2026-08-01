package queue

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEnvelopeRoundTripAndValidation(t *testing.T) {
	envelope, err := NewEnvelope(MessageJobRequested, "trace-1", "correlation-1", "causation-1", map[string]any{
		"jobId":            "22222222-2222-4222-8222-222222222222",
		"datasetVersionId": "33333333-3333-4333-8333-333333333333",
		"operations":       []any{map[string]any{"type": "PROFILE_DATASET", "config": map[string]any{}}},
	})
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

func TestEnvelopeRejectsUnknownOrMalformedMessages(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Envelope)
	}{
		{name: "unknown type", mutate: func(e *Envelope) { e.MessageType = "processing.unknown" }},
		{name: "missing trace", mutate: func(e *Envelope) { e.TraceID = "" }},
		{name: "empty operations", mutate: func(e *Envelope) {
			e.Payload = json.RawMessage(`{"jobId":"22222222-2222-4222-8222-222222222222","datasetVersionId":"33333333-3333-4333-8333-333333333333","operations":[]}`)
		}},
		{name: "nil message", mutate: func(e *Envelope) { e.MessageID = uuid.Nil }},
	}
	base, err := NewEnvelope(MessageJobRequested, "trace-1", "correlation-1", "causation-1", map[string]any{
		"jobId": "22222222-2222-4222-8222-222222222222", "datasetVersionId": "33333333-3333-4333-8333-333333333333", "operations": []any{map[string]any{"type": "PROFILE_DATASET", "config": map[string]any{}}},
	})
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

func TestUnmarshalEnvelopeRejectsUnknownFields(t *testing.T) {
	data := []byte(`{"messageId":"11111111-1111-4111-8111-111111111111","messageType":"processing.job.cancel-requested","schemaVersion":1,"occurredAt":"2026-08-01T12:00:00Z","traceId":"trace","correlationId":"correlation","causationId":"causation","payload":{"jobId":"22222222-2222-4222-8222-222222222222","reason":"user request"},"unexpected":true}`)
	if _, err := UnmarshalEnvelope(data); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

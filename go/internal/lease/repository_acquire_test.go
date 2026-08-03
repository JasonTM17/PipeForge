package lease

import (
	"encoding/json"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

func TestNewAcquireCommandIncludesCompleteFenceAndSource(t *testing.T) {
	sizeBytes := int64(4096)
	item := leaseCandidate{
		JobID:            uuid.New(),
		DatasetVersionID: uuid.New(),
		AttemptID:        uuid.New(),
		AttemptNumber:    2,
		Operations:       []byte(`[{"type":"PROFILE_DATASET","config":{"maxCommonValues":10}}]`),
		ObjectKey:        "datasets/version.csv",
		ContentType:      "text/csv",
		Format:           "CSV",
		SizeBytes:        &sizeBytes,
	}
	lease := Lease{
		LeaseID:       uuid.New(),
		JobID:         item.JobID,
		AttemptID:     item.AttemptID,
		WorkerID:      uuid.New(),
		AttemptNumber: item.AttemptNumber,
	}

	message, err := newAcquireCommand(item, lease)
	if err != nil {
		t.Fatalf("newAcquireCommand returned error: %v", err)
	}
	if message.ID != message.Envelope.MessageID || message.Exchange != queue.CommandsExchange || message.RoutingKey != queue.MessageJobRequested {
		t.Fatalf("unexpected outbox message metadata: %+v", message)
	}

	var payload struct {
		JobID            uuid.UUID       `json:"jobId"`
		DatasetVersionID uuid.UUID       `json:"datasetVersionId"`
		AttemptID        uuid.UUID       `json:"attemptId"`
		LeaseID          uuid.UUID       `json:"leaseId"`
		WorkerID         uuid.UUID       `json:"workerId"`
		AttemptNumber    int             `json:"attemptNumber"`
		Operations       []job.Operation `json:"operations"`
		Source           requestedSource `json:"source"`
	}
	if err := json.Unmarshal(message.Envelope.Payload, &payload); err != nil {
		t.Fatalf("decode requested command payload: %v", err)
	}
	if payload.JobID != item.JobID || payload.DatasetVersionID != item.DatasetVersionID ||
		payload.AttemptID != lease.AttemptID || payload.LeaseID != lease.LeaseID ||
		payload.WorkerID != lease.WorkerID || payload.AttemptNumber != lease.AttemptNumber {
		t.Fatalf("command fence fields do not match lease: %+v", payload)
	}
	if len(payload.Operations) != 1 || payload.Operations[0].Type != "PROFILE_DATASET" {
		t.Fatalf("command operations were not preserved: %+v", payload.Operations)
	}
	if payload.Source != (requestedSource{
		ObjectKey: "datasets/version.csv", ContentType: "text/csv", Format: "CSV", SizeBytes: sizeBytes,
	}) {
		t.Fatalf("command source does not match authoritative metadata: %+v", payload.Source)
	}

	var rawPayload map[string]json.RawMessage
	if err := json.Unmarshal(message.Envelope.Payload, &rawPayload); err != nil {
		t.Fatalf("decode raw requested command payload: %v", err)
	}
	var rawSource map[string]json.RawMessage
	if err := json.Unmarshal(rawPayload["source"], &rawSource); err != nil {
		t.Fatalf("decode command source: %v", err)
	}
	if len(rawSource) != 4 {
		t.Fatalf("source contains unexpected fields: %v", rawSource)
	}
	if _, ok := rawSource["originalFilename"]; ok {
		t.Fatalf("source leaked user-controlled filename: %v", rawSource)
	}
}

func TestNewAcquireCommandRejectsMissingSourceSize(t *testing.T) {
	_, err := newAcquireCommand(leaseCandidate{Operations: []byte(`[]`)}, Lease{})
	if err == nil {
		t.Fatal("newAcquireCommand accepted missing source size")
	}
}

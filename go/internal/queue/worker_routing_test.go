package queue

import (
	"testing"

	"github.com/google/uuid"
)

func TestWorkerRoutingKeyTargetsCanonicalWorkerUUID(t *testing.T) {
	workerID := uuid.MustParse("AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE")
	key, err := WorkerRoutingKey(MessageJobRequested, workerID)
	if err != nil {
		t.Fatalf("WorkerRoutingKey returned error: %v", err)
	}
	if key != "processing.job.requested.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Fatalf("unexpected worker routing key: %s", key)
	}
}

func TestWorkerRoutingKeyRejectsUnsupportedMessageOrEmptyWorker(t *testing.T) {
	if _, err := WorkerRoutingKey(MessageJobQueued, uuid.New()); err == nil {
		t.Fatal("WorkerRoutingKey accepted an untargeted message type")
	}
	if _, err := WorkerRoutingKey(MessageJobCancel, uuid.Nil); err == nil {
		t.Fatal("WorkerRoutingKey accepted an empty worker ID")
	}
}

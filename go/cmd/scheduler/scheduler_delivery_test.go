package main

import (
	"testing"

	"github.com/google/uuid"
)

func TestDecodeQueuedJobSignalRequiresOnlyANonZeroJobID(t *testing.T) {
	jobID := uuid.New()
	signal, err := decodeQueuedJobSignal([]byte(`{"jobId":"` + jobID.String() + `"}`))
	if err != nil || signal.JobID != jobID {
		t.Fatalf("decodeQueuedJobSignal returned signal=%+v err=%v", signal, err)
	}
	for _, payload := range [][]byte{
		[]byte(`{"jobId":"not-a-uuid"}`),
		[]byte(`{"jobId":"00000000-0000-0000-0000-000000000000"}`),
		[]byte(`{"jobId":"` + jobID.String() + `","unexpected":true}`),
		[]byte(`[]`),
	} {
		if _, err := decodeQueuedJobSignal(payload); err == nil {
			t.Fatalf("invalid queued signal was accepted: %s", payload)
		}
	}
}

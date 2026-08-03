package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/workerregistry"
)

type recordingWorkerEventStore struct {
	failures int
	err      error
	calls    int
}

func (s *recordingWorkerEventStore) ApplyEnvelope(context.Context, queue.Envelope) error {
	s.calls++
	if s.calls <= s.failures {
		return s.err
	}
	return nil
}

func TestApplyWorkerEnvelopeRetriesTransientPersistenceFailure(t *testing.T) {
	store := &recordingWorkerEventStore{failures: 2, err: errors.New("database unavailable")}
	if err := applyWorkerEnvelopeWithRetry(context.Background(), store, queue.Envelope{}, []time.Duration{0, 0}); err != nil {
		t.Fatalf("applyWorkerEnvelopeWithRetry returned error: %v", err)
	}
	if store.calls != 3 {
		t.Fatalf("worker event attempts=%d, want 3", store.calls)
	}
}

func TestApplyWorkerEnvelopeDoesNotRetryInvalidEvent(t *testing.T) {
	store := &recordingWorkerEventStore{failures: 1, err: workerregistry.ErrInvalidEvent}
	err := applyWorkerEnvelopeWithRetry(context.Background(), store, queue.Envelope{}, []time.Duration{0, 0})
	if !errors.Is(err, workerregistry.ErrInvalidEvent) || store.calls != 1 {
		t.Fatalf("invalid event err=%v calls=%d", err, store.calls)
	}
}

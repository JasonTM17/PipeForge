package result

import (
	"context"
	"errors"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

type fakeDelivery struct {
	acked    bool
	nacked   bool
	rejected bool
	requeue  bool
	multiple bool
}

func (d *fakeDelivery) Ack(multiple bool) error {
	d.acked, d.multiple = true, multiple
	return nil
}

func (d *fakeDelivery) Nack(multiple, requeue bool) error {
	d.nacked, d.multiple, d.requeue = true, multiple, requeue
	return nil
}

func (d *fakeDelivery) Reject(requeue bool) error {
	d.rejected, d.requeue = true, requeue
	return nil
}

type fakeProcessor struct{ err error }

func (p fakeProcessor) Process(context.Context, queue.Envelope) (Outcome, error) {
	if p.err != nil {
		return Outcome{}, p.err
	}
	return Outcome{Status: OutcomeApplied}, nil
}

func validResultBody(t *testing.T) []byte {
	t.Helper()
	payload := StartedEvent{JobID: uuid.New(), AttemptID: uuid.New(), LeaseID: uuid.New(), WorkerID: uuid.New(), AttemptNumber: 1}
	envelope, err := queue.NewEnvelope(queue.MessageJobStarted, "trace", "correlation", "causation", payload)
	if err != nil {
		t.Fatalf("NewEnvelope returned error: %v", err)
	}
	body, err := queue.MarshalEnvelope(envelope)
	if err != nil {
		t.Fatalf("MarshalEnvelope returned error: %v", err)
	}
	return body
}

func TestHandleDeliveryAcknowledgesAfterProcessing(t *testing.T) {
	delivery := &fakeDelivery{}
	if err := HandleDelivery(context.Background(), fakeProcessor{}, delivery, validResultBody(t)); err != nil {
		t.Fatalf("HandleDelivery returned error: %v", err)
	}
	if !delivery.acked || delivery.nacked || delivery.rejected || delivery.multiple {
		t.Fatalf("unexpected delivery outcome: %+v", delivery)
	}
}

func TestHandleDeliveryDeadLettersMalformedAndFailedMessages(t *testing.T) {
	malformedDelivery := &fakeDelivery{}
	if err := HandleDelivery(context.Background(), fakeProcessor{}, malformedDelivery, []byte("not-json")); err == nil {
		t.Fatal("malformed message was accepted")
	}
	if !malformedDelivery.rejected || malformedDelivery.requeue {
		t.Fatalf("malformed message was not rejected to DLQ: %+v", malformedDelivery)
	}

	failedDelivery := &fakeDelivery{}
	if err := HandleDelivery(context.Background(), fakeProcessor{err: errors.New("database unavailable")}, failedDelivery, validResultBody(t)); err == nil {
		t.Fatal("processor failure was hidden")
	}
	if !failedDelivery.nacked || failedDelivery.requeue || failedDelivery.acked || failedDelivery.rejected {
		t.Fatalf("processor failure was not dead-lettered: %+v", failedDelivery)
	}
}

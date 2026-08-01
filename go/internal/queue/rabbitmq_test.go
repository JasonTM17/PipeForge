package queue

import (
	"context"
	"testing"
	"time"
)

func TestDefaultTopologyMatchesContract(t *testing.T) {
	topology := DefaultTopology()
	if len(topology.Exchanges) != 3 || len(topology.Queues) != 8 || len(topology.Bindings) != 9 {
		t.Fatalf("unexpected topology counts: %+v", topology)
	}
	if topology.Queues[0].DeadLetter != "processing.jobs.dlq" {
		t.Fatalf("job queue does not have bounded dead-letter routing: %+v", topology.Queues[0])
	}
}

func TestPublisherConfigDefaultsAndValidation(t *testing.T) {
	if err := (PublisherConfig{URL: "amqp://guest:guest@localhost:5672/", ConfirmTimeout: time.Second, MaxMessageSize: MaxEnvelopeBytes}).validate(); err != nil {
		t.Fatalf("valid publisher config rejected: %v", err)
	}
	if err := (PublisherConfig{ConfirmTimeout: time.Second, MaxMessageSize: MaxEnvelopeBytes}).validate(); err == nil {
		t.Fatal("missing URL was accepted")
	}
}

func TestPublisherRejectsInvalidEnvelopeBeforeBrokerCall(t *testing.T) {
	publisher := &Publisher{config: PublisherConfig{ConfirmTimeout: time.Second, MaxMessageSize: MaxEnvelopeBytes}}
	err := publisher.Publish(context.Background(), CommandsExchange, "processing.job.requested", Envelope{})
	if err == nil {
		t.Fatal("invalid envelope was accepted")
	}
}

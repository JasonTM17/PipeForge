package queue

import (
	"context"
	"testing"
	"time"
)

func TestDefaultTopologyMatchesContract(t *testing.T) {
	topology := DefaultTopology()
	if len(topology.Exchanges) != 3 || len(topology.Queues) != 12 || len(topology.Bindings) != 18 {
		t.Fatalf("unexpected topology counts: %+v", topology)
	}
	if topology.Queues[0].DeadLetter != "processing.jobs.dlq" {
		t.Fatalf("job queue does not have bounded dead-letter routing: %+v", topology.Queues[0])
	}
	if !containsQueue(topology, "control-plane.dispatch", "control-plane.dispatch.dlq") {
		t.Fatalf("dispatch queue does not have bounded dead-letter routing: %+v", topology.Queues)
	}
	if !containsBinding(topology, CommandsExchange, "control-plane.dispatch", MessageJobQueued) {
		t.Fatalf("queued signal is not bound to the dispatch queue: %+v", topology.Bindings)
	}
	if !containsQueue(topology, "control-plane.workers", "control-plane.workers.dlq") ||
		!containsBinding(topology, EventsExchange, "control-plane.workers", MessageWorkerRegistered) ||
		!containsBinding(topology, EventsExchange, "control-plane.workers", MessageWorkerHeartbeat) {
		t.Fatalf("worker registry events are not durably routed: %+v", topology)
	}
	for _, messageType := range []string{MessageJobStarted, MessageJobProgressed, MessageJobSucceeded, MessageJobFailed, MessageJobCancelled, MessageArtifactCreated} {
		if !containsBinding(topology, EventsExchange, "control-plane.results", messageType) {
			t.Fatalf("result event %s is not bound to the result queue: %+v", messageType, topology.Bindings)
		}
	}
	if containsBinding(topology, EventsExchange, "control-plane.results", "processing.job.#") || containsBinding(topology, EventsExchange, "control-plane.results", "processing.#") {
		t.Fatalf("result queue must not consume non-result processing events: %+v", topology.Bindings)
	}
	if !containsBinding(topology, DeadLetterExchange, "control-plane.dispatch.dlq", "control-plane.dispatch.dlq") {
		t.Fatalf("dispatch DLQ is not bound to the dead-letter exchange: %+v", topology.Bindings)
	}
}

func containsQueue(topology Topology, name, deadLetter string) bool {
	for _, queue := range topology.Queues {
		if queue.Name == name && queue.DeadLetter == deadLetter {
			return true
		}
	}
	return false
}

func containsBinding(topology Topology, exchange, queue, key string) bool {
	for _, binding := range topology.Bindings {
		if binding.Exchange == exchange && binding.Queue == queue && binding.Key == key {
			return true
		}
	}
	return false
}

func TestPublisherConfigDefaultsAndValidation(t *testing.T) {
	if err := (PublisherConfig{URL: "amqp://guest:guest@localhost:5672/", ConfirmTimeout: time.Second, DialTimeout: time.Second, MaxMessageSize: MaxEnvelopeBytes}).validate(); err != nil {
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

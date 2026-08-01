//go:build integration

package queue

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRabbitPublisherConfirmsAndReconnects(t *testing.T) {
	url := os.Getenv("PIPEFORGE_TEST_RABBIT_URL")
	if url == "" {
		t.Skip("PIPEFORGE_TEST_RABBIT_URL is not set")
	}
	publisher, err := NewPublisher(PublisherConfig{URL: url, ConfirmTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewPublisher returned error: %v", err)
	}
	defer publisher.Close()
	if err := publisher.DeclareTopology(context.Background(), DefaultTopology()); err != nil {
		t.Fatalf("DeclareTopology returned error: %v", err)
	}
	envelope, err := NewEnvelope(MessageJobRequested, "trace-integration", "correlation-integration", "causation-integration", map[string]any{
		"jobId": "22222222-2222-4222-8222-222222222222", "datasetVersionId": "33333333-3333-4333-8333-333333333333", "operations": []any{map[string]any{"type": "PROFILE_DATASET", "config": map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), CommandsExchange, "test.publisher.confirm", envelope); err != nil {
		t.Fatalf("first publish returned error: %v", err)
	}
	if publisher.connection == nil {
		t.Fatal("publisher did not establish a connection")
	}
	_ = publisher.connection.Close()
	second, err := NewEnvelope(MessageJobCancel, "trace-integration", "correlation-integration", envelope.MessageID.String(), map[string]any{
		"jobId": "22222222-2222-4222-8222-222222222222", "reason": "integration reconnect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), CommandsExchange, "test.publisher.reconnect", second); err != nil {
		t.Fatalf("publish after broker connection close returned error: %v", err)
	}
}

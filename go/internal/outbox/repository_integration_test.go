//go:build integration

package outbox

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryEnqueueClaimAndLeaseFence(t *testing.T) {
	dsn := os.Getenv("PIPEFORGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PIPEFORGE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New returned error: %v", err)
	}
	defer pool.Close()
	repository, err := NewRepository(pool)
	if err != nil {
		t.Fatalf("NewRepository returned error: %v", err)
	}
	envelope, err := queue.NewEnvelope(queue.MessageJobCancel, "trace-integration", "correlation-integration", "causation-integration", map[string]string{
		"jobId": "77777777-7777-4777-8777-777777777777", "reason": "repository integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	message := Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType}
	if _, err := pool.Exec(ctx, "DELETE FROM outbox_messages WHERE id = $1", message.ID); err != nil {
		t.Fatalf("cleanup pre-existing fixture failed: %v", err)
	}
	defer pool.Exec(ctx, "DELETE FROM outbox_messages WHERE id = $1", message.ID)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Enqueue(ctx, tx, message); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Enqueue returned error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("outbox transaction commit failed: %v", err)
	}
	claimed, err := repository.Claim(ctx, time.Now().UTC(), time.Minute, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim returned claimed=%+v err=%v", claimed, err)
	}
	second, err := repository.Claim(ctx, time.Now().UTC(), time.Minute, 10)
	if err != nil {
		t.Fatalf("second Claim returned error: %v", err)
	} else if len(second) != 0 {
		t.Fatalf("leased message was claimed twice: %+v", second)
	}
	if err := repository.MarkPublished(ctx, message.ID, uuid.New(), time.Now().UTC()); err != ErrLeaseLost {
		t.Fatalf("wrong lease token was not rejected: %v", err)
	}
	if err := repository.MarkPublished(ctx, message.ID, claimed[0].LeaseToken, time.Now().UTC()); err != nil {
		t.Fatalf("valid lease token was rejected: %v", err)
	}
}

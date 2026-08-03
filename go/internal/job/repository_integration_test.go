//go:build integration

package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryCreatesAtomicIdempotentJobAndOutbox(t *testing.T) {
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
	outboxRepository, err := outbox.NewRepository(pool)
	if err != nil {
		t.Fatalf("outbox.NewRepository returned error: %v", err)
	}
	repository, err := NewRepository(pool, outboxRepository)
	if err != nil {
		t.Fatalf("NewRepository returned error: %v", err)
	}
	ownerID, datasetID, versionID := uuid.New(), uuid.New(), uuid.New()
	insertFixture(t, ctx, pool, ownerID, datasetID, versionID)
	defer cleanupFixture(t, ctx, pool, ownerID, datasetID)

	command := CreateCommand{
		OwnerUserID: ownerID, ActorUserID: ownerID, DatasetVersionID: versionID,
		Operations:     []Operation{{Type: "PROFILE_DATASET", Config: map[string]any{}}},
		IdempotencyKey: "integration-job-1", MaxAttempts: 5, TraceID: "integration-trace",
	}
	created, replayed, err := repository.Create(ctx, command)
	if err != nil || replayed {
		t.Fatalf("initial Create returned job=%+v replayed=%v err=%v", created, replayed, err)
	}
	if created.State != StateQueued || created.ID == uuid.Nil {
		t.Fatalf("unexpected created job: %+v", created)
	}
	replayedJob, replayed, err := repository.Create(ctx, command)
	if err != nil || !replayed || replayedJob.ID != created.ID {
		t.Fatalf("idempotent Create returned job=%+v replayed=%v err=%v", replayedJob, replayed, err)
	}
	conflict := command
	conflict.Operations = []Operation{{Type: "CHECK_MISSING_VALUES", Config: map[string]any{"columns": []string{"email"}}}}
	if _, _, err := repository.Create(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected deterministic idempotency conflict, got %v", err)
	}

	var jobs, attempts, histories, idempotency, outboxRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE id = $1`, created.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_attempts WHERE job_id = $1`, created.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_state_history WHERE job_id = $1`, created.ID).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_idempotency_records WHERE job_id = $1`, created.ID).Scan(&idempotency); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_messages WHERE payload->>'jobId' = $1`, created.ID.String()).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || attempts != 1 || histories != 1 || idempotency != 1 || outboxRows != 1 {
		t.Fatalf("atomic job records incomplete: jobs=%d attempts=%d histories=%d idempotency=%d outbox=%d", jobs, attempts, histories, idempotency, outboxRows)
	}
	var messageType, exchange, routingKey string
	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT message_type, exchange, routing_key, payload
FROM outbox_messages
WHERE payload->>'jobId' = $1`, created.ID.String()).Scan(&messageType, &exchange, &routingKey, &payload); err != nil {
		t.Fatal(err)
	}
	if messageType != queue.MessageJobQueued || exchange != queue.CommandsExchange || routingKey != queue.MessageJobQueued {
		t.Fatalf("job creation emitted the wrong outbox route: type=%q exchange=%q routingKey=%q", messageType, exchange, routingKey)
	}
	var queuedPayload map[string]json.RawMessage
	if err := json.Unmarshal(payload, &queuedPayload); err != nil {
		t.Fatalf("decode queued outbox payload: %v", err)
	}
	if len(queuedPayload) != 1 {
		t.Fatalf("job creation emitted executable payload instead of job-only queued signal: %s", payload)
	}
	var queuedJobID string
	if err := json.Unmarshal(queuedPayload["jobId"], &queuedJobID); err != nil || queuedJobID != created.ID.String() {
		t.Fatalf("job creation emitted the wrong queued job ID: payload=%s err=%v", payload, err)
	}

	queued, err := repository.SelectQueued(ctx, QueueQuery{Limit: 10})
	if err != nil || len(queued) != 1 || queued[0].ID != created.ID {
		t.Fatalf("SelectQueued returned jobs=%+v err=%v", queued, err)
	}
	cancelled, err := repository.RequestCancel(ctx, CancelCommand{JobID: created.ID, ActorUserID: ownerID, TraceID: "cancel-trace", RequestID: "cancel-request"})
	if err != nil || cancelled.State != StateCancelled {
		t.Fatalf("RequestCancel returned job=%+v err=%v", cancelled, err)
	}
	queued, err = repository.SelectQueued(ctx, QueueQuery{Limit: 10})
	if err != nil || len(queued) != 0 {
		t.Fatalf("cancelled job remained queued: jobs=%+v err=%v", queued, err)
	}
}

func TestRepositoryRollsBackWhenOutboxEnqueueFails(t *testing.T) {
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
	ownerID, datasetID, versionID := uuid.New(), uuid.New(), uuid.New()
	insertFixture(t, ctx, pool, ownerID, datasetID, versionID)
	defer cleanupFixture(t, ctx, pool, ownerID, datasetID)
	repository, err := NewRepository(pool, failingEnqueuer{err: errors.New("outbox unavailable")})
	if err != nil {
		t.Fatalf("NewRepository returned error: %v", err)
	}
	_, _, err = repository.Create(ctx, CreateCommand{
		OwnerUserID: ownerID, ActorUserID: ownerID, DatasetVersionID: versionID,
		Operations:  []Operation{{Type: "PROFILE_DATASET", Config: map[string]any{}}},
		MaxAttempts: 5, TraceID: "rollback-trace",
	})
	if err == nil {
		t.Fatal("Create succeeded despite outbox failure")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE owner_user_id = $1`, ownerID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("job transaction was not rolled back, count=%d", count)
	}
}

func TestRepositoryRoutesActiveCancellationAndRetryToScheduler(t *testing.T) {
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
	outboxRepository, err := outbox.NewRepository(pool)
	if err != nil {
		t.Fatalf("outbox.NewRepository returned error: %v", err)
	}
	repository, err := NewRepository(pool, outboxRepository)
	if err != nil {
		t.Fatalf("NewRepository returned error: %v", err)
	}
	ownerID, datasetID, versionID := uuid.New(), uuid.New(), uuid.New()
	insertFixture(t, ctx, pool, ownerID, datasetID, versionID)
	defer cleanupFixture(t, ctx, pool, ownerID, datasetID)

	create := func(key string) Job {
		item, _, createErr := repository.Create(ctx, CreateCommand{
			OwnerUserID: ownerID, ActorUserID: ownerID, DatasetVersionID: versionID,
			Operations:     []Operation{{Type: "PROFILE_DATASET", Config: map[string]any{}}},
			IdempotencyKey: key, MaxAttempts: 5, TraceID: key,
		})
		if createErr != nil {
			t.Fatalf("Create(%s) returned error: %v", key, createErr)
		}
		if _, deleteErr := pool.Exec(ctx, `DELETE FROM outbox_messages WHERE payload->>'jobId' = $1`, item.ID.String()); deleteErr != nil {
			t.Fatalf("delete initial queued message: %v", deleteErr)
		}
		return item
	}

	cancelJob := create("integration-cancel-routing")
	workerID, leaseID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `UPDATE processing_jobs SET state = 'LEASED' WHERE id = $1`, cancelJob.ID); err != nil {
		t.Fatalf("lease cancellation fixture job: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE job_attempts SET state = 'LEASED', worker_id = $2, lease_id = $3, leased_at = NOW(), lease_expires_at = NOW() + INTERVAL '1 minute' WHERE job_id = $1`, cancelJob.ID, workerID, leaseID); err != nil {
		t.Fatalf("lease cancellation fixture attempt: %v", err)
	}
	if _, err := repository.RequestCancel(ctx, CancelCommand{JobID: cancelJob.ID, ActorUserID: ownerID}); err != nil {
		t.Fatalf("RequestCancel returned error: %v", err)
	}
	var cancelType, cancelRoute string
	if err := pool.QueryRow(ctx, `SELECT message_type, routing_key FROM outbox_messages WHERE payload->>'jobId' = $1`, cancelJob.ID.String()).Scan(&cancelType, &cancelRoute); err != nil {
		t.Fatalf("select cancellation outbox message: %v", err)
	}
	expectedCancelRoute, err := queue.WorkerRoutingKey(queue.MessageJobCancel, workerID)
	if err != nil {
		t.Fatalf("WorkerRoutingKey returned error: %v", err)
	}
	if cancelType != queue.MessageJobCancel || cancelRoute != expectedCancelRoute {
		t.Fatalf("active cancellation route type=%q route=%q, want type=%q route=%q", cancelType, cancelRoute, queue.MessageJobCancel, expectedCancelRoute)
	}

	retryJob := create("integration-retry-routing")
	if _, err := pool.Exec(ctx, `UPDATE processing_jobs SET state = 'FAILED_PERMANENT' WHERE id = $1`, retryJob.ID); err != nil {
		t.Fatalf("fail retry fixture job: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE job_attempts SET state = 'FAILED', finished_at = NOW() WHERE job_id = $1`, retryJob.ID); err != nil {
		t.Fatalf("fail retry fixture attempt: %v", err)
	}
	if _, err := repository.RequestRetry(ctx, RetryCommand{JobID: retryJob.ID, ActorUserID: ownerID}); err != nil {
		t.Fatalf("RequestRetry returned error: %v", err)
	}
	var retryType, retryRoute string
	if err := pool.QueryRow(ctx, `SELECT message_type, routing_key FROM outbox_messages WHERE payload->>'jobId' = $1`, retryJob.ID.String()).Scan(&retryType, &retryRoute); err != nil {
		t.Fatalf("select retry outbox message: %v", err)
	}
	if retryType != queue.MessageJobQueued || retryRoute != queue.MessageJobQueued {
		t.Fatalf("retry route type=%q route=%q, want scheduler queued signal", retryType, retryRoute)
	}
}

type failingEnqueuer struct{ err error }

func (f failingEnqueuer) Enqueue(context.Context, pgx.Tx, outbox.Message) error {
	return f.err
}

func insertFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, datasetID, versionID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'integration')`, ownerID, fmt.Sprintf("job-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert job fixture user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO datasets (id, owner_user_id, name, state) VALUES ($1, $2, $3, 'AVAILABLE')`, datasetID, ownerID, "job-"+datasetID.String()); err != nil {
		t.Fatalf("insert job fixture dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO dataset_versions (id, dataset_id, version_number, state, original_filename, content_type, format, object_key, artifact_prefix, created_by, available_at)
VALUES ($1, $2, 1, 'AVAILABLE', 'fixture.csv', 'text/csv', 'CSV', $3, $4, $5, NOW())`, versionID, datasetID, "fixtures/"+versionID.String(), "artifacts/"+versionID.String(), ownerID); err != nil {
		t.Fatalf("insert job fixture version: %v", err)
	}
}

func cleanupFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, datasetID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM outbox_messages WHERE payload->>'jobId' IN (SELECT id::text FROM processing_jobs WHERE owner_user_id = $1)`, ownerID); err != nil {
		t.Errorf("delete job fixture outbox messages: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM processing_jobs WHERE owner_user_id = $1`, ownerID); err != nil {
		t.Errorf("delete job fixture jobs: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM job_quota_counters WHERE owner_user_id = $1`, ownerID); err != nil {
		t.Errorf("delete job fixture quota: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM datasets WHERE id = $1`, datasetID); err != nil {
		t.Errorf("delete job fixture dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
		t.Errorf("delete job fixture user: %v", err)
	}
}

//go:build integration

package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
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
		Operations: []Operation{{Type: "PROFILE_DATASET", Config: map[string]any{}}},
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

	queued, err := repository.SelectQueued(ctx, QueueQuery{Limit: 10})
	if err != nil || len(queued) != 1 || queued[0].ID != created.ID {
		t.Fatalf("SelectQueued returned jobs=%+v err=%v", queued, err)
	}
	cancelled, err := repository.RequestCancel(ctx, CancelCommand{JobID: created.ID, ActorUserID: ownerID, TraceID: "cancel-trace", RequestID: "cancel-request"})
	if err != nil || cancelled.State != StateCancelRequested {
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
		Operations: []Operation{{Type: "PROFILE_DATASET", Config: map[string]any{}}},
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

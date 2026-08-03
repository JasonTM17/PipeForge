//go:build integration

package workerregistry

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationClock struct{ now time.Time }

func (c *integrationClock) Now() time.Time { return c.now }

func TestRepositoryKeepsWorkerEventsMonotonicAndSelectsEligibleWorkers(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := &integrationClock{now: now}
	repository, err := NewRepository(pool, Config{HeartbeatTTL: time.Minute, Clock: clock})
	if err != nil {
		t.Fatalf("NewRepository returned error: %v", err)
	}
	firstID, secondID := uuid.New(), uuid.New()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM processing_workers WHERE worker_id = ANY($1::uuid[])`, []uuid.UUID{firstID, secondID})
	}()

	registerWorker(t, ctx, repository, firstID, "first", now, []string{"PROFILE_DATASET"}, 1)
	heartbeatWorker(t, ctx, repository, firstID, "first", now.Add(time.Second), StatusReady, []string{"PROFILE_DATASET"}, 1)
	clock.now = now.Add(2 * time.Second)
	heartbeatWorker(t, ctx, repository, firstID, "first", now.Add(-time.Second), StatusOffline, []string{"PROFILE_DATASET"}, 1)
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM processing_workers WHERE worker_id = $1`, firstID).Scan(&status); err != nil {
		t.Fatalf("select worker status: %v", err)
	}
	if status != StatusReady {
		t.Fatalf("stale heartbeat moved status backwards: %s", status)
	}
	registerWorker(t, ctx, repository, firstID, "first", now, []string{"PROFILE_DATASET"}, 1)
	if err := pool.QueryRow(ctx, `SELECT status FROM processing_workers WHERE worker_id = $1`, firstID).Scan(&status); err != nil {
		t.Fatalf("select worker status after duplicate registration: %v", err)
	}
	if status != StatusReady {
		t.Fatalf("duplicate registration reset live worker status: %s", status)
	}

	registerWorker(t, ctx, repository, secondID, "second", now, []string{"PROFILE_DATASET", "DETECT_OUTLIERS"}, 1)
	heartbeatWorker(t, ctx, repository, secondID, "second", now.Add(time.Second), StatusReady, []string{"PROFILE_DATASET", "DETECT_OUTLIERS"}, 1)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin worker selection: %v", err)
	}
	selected, available, err := repository.SelectForLease(ctx, tx, []string{"DETECT_OUTLIERS"}, clock.now)
	if err != nil || !available || selected != secondID {
		_ = tx.Rollback(ctx)
		t.Fatalf("capability selection worker=%s available=%t err=%v", selected, available, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit worker selection: %v", err)
	}

	clock.now = now.Add(2 * time.Minute)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin stale selection: %v", err)
	}
	_, available, err = repository.SelectForLease(ctx, tx, []string{"PROFILE_DATASET"}, clock.now)
	_ = tx.Rollback(ctx)
	if err != nil || available {
		t.Fatalf("stale workers remained eligible: available=%t err=%v", available, err)
	}
}

func registerWorker(t *testing.T, ctx context.Context, repository *Repository, workerID uuid.UUID, instanceID string, startedAt time.Time, operations []string, maxConcurrency int) {
	t.Helper()
	if err := repository.ApplyRegistration(ctx, Registration{
		WorkerID: workerID, InstanceID: instanceID, Hostname: instanceID + ".local",
		SupportedOperations: operations, SoftwareVersion: "integration", MaxConcurrency: maxConcurrency, StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("ApplyRegistration returned error: %v", err)
	}
}

func heartbeatWorker(t *testing.T, ctx context.Context, repository *Repository, workerID uuid.UUID, instanceID string, observedAt time.Time, status string, operations []string, maxConcurrency int) {
	t.Helper()
	if err := repository.ApplyHeartbeat(ctx, Heartbeat{
		WorkerID: workerID, InstanceID: instanceID, Hostname: instanceID + ".local",
		SupportedOperations: operations, SoftwareVersion: "integration", Status: status,
		CurrentConcurrency: 0, MaxConcurrency: maxConcurrency, ObservedAt: observedAt,
	}); err != nil {
		t.Fatalf("ApplyHeartbeat returned error: %v", err)
	}
}

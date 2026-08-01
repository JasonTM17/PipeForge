//go:build integration

package lease

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationClock struct{ now time.Time }

func (c *integrationClock) Now() time.Time { return c.now }

func TestRepositoryAcquiresRenewsRecoversAndDeadLettersLeases(t *testing.T) {
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
		t.Fatalf("New outbox repository: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := &integrationClock{now: now}
	policy := retry.Policy{MaxAttempts: 2, Delays: []time.Duration{0, 10 * time.Second}, JitterFraction: 0}
	repository, err := NewRepository(pool, outboxRepository, Config{LeaseDuration: 10 * time.Second, RenewalWindow: 10 * time.Second, SweepLimit: 10, RetryPolicy: policy, Clock: clock})
	if err != nil {
		t.Fatalf("New lease repository: %v", err)
	}

	ownerID, datasetID, versionID, jobID, attemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertLeaseFixture(t, ctx, pool, ownerID, datasetID, versionID, jobID, attemptID)
	defer cleanupLeaseFixture(t, ctx, pool, ownerID, datasetID, jobID)

	first, err := repository.Acquire(ctx, AcquireCommand{JobID: jobID, WorkerID: uuid.New()})
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if first.AttemptID != attemptID || first.LeaseID == uuid.Nil {
		t.Fatalf("unexpected acquired lease: %+v", first)
	}
	renewed, err := repository.Renew(ctx, RenewCommand{LeaseID: first.LeaseID, WorkerID: first.WorkerID})
	if err != nil || renewed.LeaseExpiresAt.Before(first.LeaseExpiresAt) {
		t.Fatalf("Renew returned lease=%+v err=%v", renewed, err)
	}

	clock.now = now.Add(20 * time.Second)
	recovered, err := repository.SweepExpired(ctx, 10)
	if err != nil || recovered.Retried != 1 || recovered.DeadLettered != 0 {
		t.Fatalf("first sweep report=%+v err=%v", recovered, err)
	}
	clock.now = now.Add(30 * time.Second)
	second, err := repository.Acquire(ctx, AcquireCommand{JobID: jobID, WorkerID: uuid.New()})
	if err != nil {
		t.Fatalf("Acquire retry returned error: %v", err)
	}
	clock.now = now.Add(50 * time.Second)
	dead, err := repository.SweepExpired(ctx, 10)
	if err != nil || dead.DeadLettered != 1 {
		t.Fatalf("second sweep report=%+v err=%v", dead, err)
	}

	var jobState, attemptCount, deadLetters, outboxRows string
	if err := pool.QueryRow(ctx, `SELECT state FROM processing_jobs WHERE id = $1`, jobID).Scan(&jobState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::text FROM job_attempts WHERE job_id = $1`, jobID).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::text FROM job_dead_letters WHERE job_id = $1`, jobID).Scan(&deadLetters); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::text FROM outbox_messages WHERE payload->>'jobId' = $1`, jobID.String()).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if jobState != "DEAD_LETTERED" || attemptCount != "2" || deadLetters != "1" || outboxRows != "1" || second.AttemptNumber != 2 {
		t.Fatalf("lease recovery incomplete: state=%s attempts=%s deadLetters=%s outbox=%s second=%+v", jobState, attemptCount, deadLetters, outboxRows, second)
	}
}

func insertLeaseFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, datasetID, versionID, jobID, attemptID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'integration')`, ownerID, fmt.Sprintf("lease-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert lease user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO datasets (id, owner_user_id, name, state) VALUES ($1, $2, $3, 'AVAILABLE')`, datasetID, ownerID, "lease-"+datasetID.String()); err != nil {
		t.Fatalf("insert lease dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO dataset_versions (id, dataset_id, version_number, state, original_filename, content_type, format, object_key, artifact_prefix, created_by, available_at) VALUES ($1, $2, 1, 'AVAILABLE', 'fixture.csv', 'text/csv', 'CSV', $3, $4, $5, NOW())`, versionID, datasetID, "fixtures/"+versionID.String(), "artifacts/"+versionID.String(), ownerID); err != nil {
		t.Fatalf("insert lease version: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO processing_jobs (id, owner_user_id, dataset_version_id, state, operations, request_fingerprint, priority, max_attempts, queued_at) VALUES ($1, $2, $3, 'QUEUED', '[{"type":"PROFILE_DATASET","config":{}}]'::jsonb, repeat('b', 64), 0, 2, NOW())`, jobID, ownerID, versionID); err != nil {
		t.Fatalf("insert lease job: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job_attempts (id, job_id, attempt_number, state, created_at) VALUES ($1, $2, 1, 'CREATED', NOW())`, attemptID, jobID); err != nil {
		t.Fatalf("insert lease attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job_quota_counters (owner_user_id, queued_count, active_count) VALUES ($1, 1, 0)`, ownerID); err != nil {
		t.Fatalf("insert lease quota: %v", err)
	}
}

func cleanupLeaseFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, datasetID, jobID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM outbox_messages WHERE payload->>'jobId' = $1`, jobID.String()); err != nil {
		t.Errorf("delete lease outbox: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM processing_jobs WHERE id = $1`, jobID); err != nil {
		t.Errorf("delete lease job: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM job_quota_counters WHERE owner_user_id = $1`, ownerID); err != nil {
		t.Errorf("delete lease quota: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM datasets WHERE id = $1`, datasetID); err != nil {
		t.Errorf("delete lease dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
		t.Errorf("delete lease user: %v", err)
	}
}

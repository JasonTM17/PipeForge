//go:build integration

package result

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryAppliesAndDeduplicatesResultEvents(t *testing.T) {
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

	ownerID, datasetID, versionID := uuid.New(), uuid.New(), uuid.New()
	jobID, attemptID, leaseID, workerID, artifactID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	objectKey := "reports/" + jobID.String() + ".json"
	insertResultFixture(t, ctx, pool, ownerID, datasetID, versionID, jobID, attemptID, leaseID, workerID)
	defer cleanupResultFixture(t, ctx, pool, ownerID, datasetID, jobID)

	artifactEnvelope, err := queue.NewEnvelope(queue.MessageArtifactCreated, "trace", jobID.String(), "command", ArtifactCreatedEvent{
		JobID: jobID, AttemptID: attemptID, LeaseID: leaseID, ArtifactID: artifactID, Kind: "profile", ObjectKey: objectKey,
		SizeBytes: 7, ContentType: "application/json", ChecksumSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("artifact envelope: %v", err)
	}
	if outcome, err := repository.Process(ctx, artifactEnvelope); err != nil || outcome.Status != OutcomeApplied {
		t.Fatalf("artifact result outcome=%+v err=%v", outcome, err)
	}

	succeededEnvelope, err := queue.NewEnvelope(queue.MessageJobSucceeded, "trace", jobID.String(), artifactEnvelope.MessageID.String(), SucceededEvent{
		JobID: jobID, AttemptID: attemptID, LeaseID: leaseID, WorkerID: workerID, Artifacts: []string{objectKey},
	})
	if err != nil {
		t.Fatalf("success envelope: %v", err)
	}
	if outcome, err := repository.Process(ctx, succeededEnvelope); err != nil || outcome.Status != OutcomeApplied {
		t.Fatalf("success result outcome=%+v err=%v", outcome, err)
	}
	duplicate, err := repository.Process(ctx, succeededEnvelope)
	if err != nil || duplicate.Status != OutcomeDuplicate {
		t.Fatalf("duplicate result outcome=%+v err=%v", duplicate, err)
	}

	var jobState, attemptState, artifactState string
	if err := pool.QueryRow(ctx, `SELECT state FROM processing_jobs WHERE id = $1`, jobID).Scan(&jobState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM job_attempts WHERE id = $1`, attemptID).Scan(&attemptState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM job_artifacts WHERE id = $1`, artifactID).Scan(&artifactState); err != nil {
		t.Fatal(err)
	}
	if jobState != "SUCCEEDED" || attemptState != "SUCCEEDED" || artifactState != "CANONICAL" {
		t.Fatalf("result transition incomplete: job=%s attempt=%s artifact=%s", jobState, attemptState, artifactState)
	}
	var activeCount int
	if err := pool.QueryRow(ctx, `SELECT active_count FROM job_quota_counters WHERE owner_user_id = $1`, ownerID).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 0 {
		t.Fatalf("successful job kept an active quota slot: active=%d", activeCount)
	}

	var inboxCount, historyCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inbox_messages WHERE message_id IN ($1, $2)`, artifactEnvelope.MessageID, succeededEnvelope.MessageID).Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_state_history WHERE job_id = $1`, jobID).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 2 || historyCount != 1 {
		t.Fatalf("inbox/history were not deduplicated: inbox=%d history=%d", inboxCount, historyCount)
	}
}

func TestRepositorySchedulesRetryableFailureThroughDelayedOutbox(t *testing.T) {
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
	repository, err := NewRepository(pool, outboxRepository)
	if err != nil {
		t.Fatalf("NewRepository returned error: %v", err)
	}
	ownerID, datasetID, versionID := uuid.New(), uuid.New(), uuid.New()
	jobID, attemptID, leaseID, workerID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertResultFixture(t, ctx, pool, ownerID, datasetID, versionID, jobID, attemptID, leaseID, workerID)
	defer cleanupResultFixture(t, ctx, pool, ownerID, datasetID, jobID)

	envelope, err := queue.NewEnvelope(queue.MessageJobFailed, "trace", jobID.String(), "command", FailedEvent{JobID: jobID, AttemptID: attemptID, LeaseID: leaseID, WorkerID: workerID, Error: FailureInfo{Code: "NETWORK_TIMEOUT", Message: "object store timed out", Retryable: true}})
	if err != nil {
		t.Fatalf("failure envelope: %v", err)
	}
	outcome, err := repository.Process(ctx, envelope)
	if err != nil || outcome.Status != OutcomeApplied || outcome.Reason != "retry_scheduled" {
		t.Fatalf("retry result outcome=%+v err=%v", outcome, err)
	}
	var state string
	var attempts, outboxRows int
	if err := pool.QueryRow(ctx, `SELECT state FROM processing_jobs WHERE id = $1`, jobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_attempts WHERE job_id = $1`, jobID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_messages WHERE payload->>'jobId' = $1`, jobID.String()).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" || attempts != 2 || outboxRows != 1 {
		t.Fatalf("retry was not scheduled: state=%s attempts=%d outbox=%d", state, attempts, outboxRows)
	}
}

func insertResultFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, datasetID, versionID, jobID, attemptID, leaseID, workerID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'integration')`, ownerID, fmt.Sprintf("result-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert result fixture user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO datasets (id, owner_user_id, name, state) VALUES ($1, $2, $3, 'AVAILABLE')`, datasetID, ownerID, "result-"+datasetID.String()); err != nil {
		t.Fatalf("insert result fixture dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO dataset_versions (id, dataset_id, version_number, state, original_filename, content_type, format, object_key, artifact_prefix, created_by, available_at)
VALUES ($1, $2, 1, 'AVAILABLE', 'fixture.csv', 'text/csv', 'CSV', $3, $4, $5, NOW())`, versionID, datasetID, "fixtures/"+versionID.String(), "artifacts/"+versionID.String(), ownerID); err != nil {
		t.Fatalf("insert result fixture version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO processing_jobs (id, owner_user_id, dataset_version_id, state, operations, request_fingerprint, priority, max_attempts)
VALUES ($1, $2, $3, 'RUNNING', '[]'::jsonb, repeat('a', 64), 0, 5)`, jobID, ownerID, versionID); err != nil {
		t.Fatalf("insert result fixture job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO job_attempts (id, job_id, attempt_number, state, lease_id, worker_id, started_at)
VALUES ($1, $2, 1, 'RUNNING', $3, $4, $5)`, attemptID, jobID, leaseID, workerID, time.Now().UTC()); err != nil {
		t.Fatalf("insert result fixture attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO job_quota_counters (owner_user_id, queued_count, active_count) VALUES ($1, 0, 1)`, ownerID); err != nil {
		t.Fatalf("insert result fixture quota: %v", err)
	}
}

func cleanupResultFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, datasetID, jobID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM inbox_messages WHERE payload->>'jobId' = $1`, jobID.String()); err != nil {
		t.Errorf("delete result inbox fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM processing_jobs WHERE id = $1`, jobID); err != nil {
		t.Errorf("delete result job fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM job_quota_counters WHERE owner_user_id = $1`, ownerID); err != nil {
		t.Errorf("delete result quota fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM datasets WHERE id = $1`, datasetID); err != nil {
		t.Errorf("delete result dataset fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID); err != nil {
		t.Errorf("delete result user fixture: %v", err)
	}
}

//go:build integration

package lease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/retry"
	"github.com/JasonTM17/PipeForge/go/internal/workerregistry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationClock struct{ now time.Time }

func (c *integrationClock) Now() time.Time { return c.now }

type fixedWorkerSelector struct{ workerID uuid.UUID }

func (s fixedWorkerSelector) SelectForLease(context.Context, pgx.Tx, []string, time.Time) (uuid.UUID, bool, error) {
	return s.workerID, s.workerID != uuid.Nil, nil
}

var errEnqueueAfterWrite = errors.New("enqueue failed after writing outbox message")

type enqueueThenFail struct{ writer outbox.Enqueuer }

func (f enqueueThenFail) Enqueue(ctx context.Context, tx pgx.Tx, message outbox.Message) error {
	if err := f.writer.Enqueue(ctx, tx, message); err != nil {
		return err
	}
	return errEnqueueAfterWrite
}

func TestRepositoryAcquireWritesFencedOutboxCommandAtomically(t *testing.T) {
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

	t.Run("acquired lease includes a complete fence and source", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		workerID := uuid.New()
		repository, err := NewRepository(pool, outboxRepository, Config{Clock: &integrationClock{now: now}, WorkerSelector: fixedWorkerSelector{workerID: workerID}})
		if err != nil {
			t.Fatalf("New lease repository: %v", err)
		}
		ownerID, datasetID, versionID, jobID, attemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
		insertLeaseFixture(t, ctx, pool, ownerID, datasetID, versionID, jobID, attemptID)
		defer cleanupLeaseFixture(t, ctx, pool, ownerID, datasetID, jobID)

		lease, err := repository.Acquire(ctx, AcquireCommand{JobID: jobID})
		if err != nil {
			t.Fatalf("Acquire returned error: %v", err)
		}
		assertAcquiredLeaseOutbox(t, ctx, pool, jobID, versionID, lease)
	})

	t.Run("failed acquire rolls back the outbox command and lease state", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		repository, err := NewRepository(pool, enqueueThenFail{writer: outboxRepository}, Config{Clock: &integrationClock{now: now}, WorkerSelector: fixedWorkerSelector{workerID: uuid.New()}})
		if err != nil {
			t.Fatalf("New lease repository: %v", err)
		}
		ownerID, datasetID, versionID, jobID, attemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
		insertLeaseFixture(t, ctx, pool, ownerID, datasetID, versionID, jobID, attemptID)
		defer cleanupLeaseFixture(t, ctx, pool, ownerID, datasetID, jobID)

		_, err = repository.Acquire(ctx, AcquireCommand{JobID: jobID})
		if !errors.Is(err, errEnqueueAfterWrite) {
			t.Fatalf("Acquire error = %v, want %v", err, errEnqueueAfterWrite)
		}
		assertFailedAcquireRolledBack(t, ctx, pool, jobID)
	})
}

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
	repository, err := NewRepository(pool, outboxRepository, Config{LeaseDuration: 10 * time.Second, RenewalWindow: 10 * time.Second, SweepLimit: 10, RetryPolicy: policy, Clock: clock, WorkerSelector: fixedWorkerSelector{workerID: uuid.New()}})
	if err != nil {
		t.Fatalf("New lease repository: %v", err)
	}

	ownerID, datasetID, versionID, jobID, attemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertLeaseFixture(t, ctx, pool, ownerID, datasetID, versionID, jobID, attemptID)
	defer cleanupLeaseFixture(t, ctx, pool, ownerID, datasetID, jobID)

	first, err := repository.Acquire(ctx, AcquireCommand{JobID: jobID})
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
	second, err := repository.Acquire(ctx, AcquireCommand{JobID: jobID})
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
	if jobState != "DEAD_LETTERED" || attemptCount != "2" || deadLetters != "1" || outboxRows != "3" || second.AttemptNumber != 2 {
		t.Fatalf("lease recovery incomplete: state=%s attempts=%s deadLetters=%s outbox=%s second=%+v", jobState, attemptCount, deadLetters, outboxRows, second)
	}
}

func TestRepositoryConcurrentAcquireDoesNotOversubscribeWorker(t *testing.T) {
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
	workers, err := workerregistry.NewRepository(pool, workerregistry.Config{HeartbeatTTL: time.Minute, Clock: clock})
	if err != nil {
		t.Fatalf("New worker registry: %v", err)
	}
	workerID := uuid.New()
	if err := workers.ApplyRegistration(ctx, workerregistry.Registration{
		WorkerID: workerID, InstanceID: "capacity-instance", Hostname: "capacity.local",
		SupportedOperations: []string{"PROFILE_DATASET"}, SoftwareVersion: "integration",
		MaxConcurrency: 1, StartedAt: now,
	}); err != nil {
		t.Fatalf("ApplyRegistration returned error: %v", err)
	}
	if err := workers.ApplyHeartbeat(ctx, workerregistry.Heartbeat{
		WorkerID: workerID, InstanceID: "capacity-instance", Hostname: "capacity.local",
		SupportedOperations: []string{"PROFILE_DATASET"}, SoftwareVersion: "integration",
		Status: workerregistry.StatusReady, MaxConcurrency: 1, ObservedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("ApplyHeartbeat returned error: %v", err)
	}
	repository, err := NewRepository(pool, outboxRepository, Config{Clock: clock, WorkerSelector: workers})
	if err != nil {
		t.Fatalf("New lease repository: %v", err)
	}
	firstOwner, firstDataset, firstVersion, firstJob, firstAttempt := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	secondOwner, secondDataset, secondVersion, secondJob, secondAttempt := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	insertLeaseFixture(t, ctx, pool, firstOwner, firstDataset, firstVersion, firstJob, firstAttempt)
	insertLeaseFixture(t, ctx, pool, secondOwner, secondDataset, secondVersion, secondJob, secondAttempt)
	defer cleanupLeaseFixture(t, ctx, pool, firstOwner, firstDataset, firstJob)
	defer cleanupLeaseFixture(t, ctx, pool, secondOwner, secondDataset, secondJob)
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM processing_workers WHERE worker_id = $1`, workerID) }()

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, jobID := range []uuid.UUID{firstJob, secondJob} {
		go func(id uuid.UUID) {
			<-start
			_, acquireErr := repository.Acquire(ctx, AcquireCommand{JobID: id})
			results <- acquireErr
		}(jobID)
	}
	close(start)
	succeeded, unavailable := 0, 0
	for range 2 {
		acquireErr := <-results
		switch {
		case acquireErr == nil:
			succeeded++
		case errors.Is(acquireErr, ErrWorkerUnavailable):
			unavailable++
		default:
			t.Fatalf("unexpected concurrent acquire error: %v", acquireErr)
		}
	}
	if succeeded != 1 || unavailable != 1 {
		t.Fatalf("capacity race results succeeded=%d unavailable=%d", succeeded, unavailable)
	}
}

func assertAcquiredLeaseOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID, versionID uuid.UUID, lease Lease) {
	t.Helper()
	var messageType, exchange, routingKey string
	var encodedPayload []byte
	if err := pool.QueryRow(ctx, `
SELECT message_type, exchange, routing_key, payload
FROM outbox_messages
WHERE payload->>'jobId' = $1`, jobID.String()).Scan(&messageType, &exchange, &routingKey, &encodedPayload); err != nil {
		t.Fatalf("select acquired lease outbox message: %v", err)
	}
	expectedRoutingKey, err := queue.WorkerRoutingKey(queue.MessageJobRequested, lease.WorkerID)
	if err != nil {
		t.Fatalf("WorkerRoutingKey returned error: %v", err)
	}
	if messageType != queue.MessageJobRequested || exchange != queue.CommandsExchange || routingKey != expectedRoutingKey {
		t.Fatalf("unexpected acquired lease routing: type=%s exchange=%s routing=%s", messageType, exchange, routingKey)
	}

	var payload struct {
		JobID            uuid.UUID         `json:"jobId"`
		DatasetVersionID uuid.UUID         `json:"datasetVersionId"`
		AttemptID        uuid.UUID         `json:"attemptId"`
		LeaseID          uuid.UUID         `json:"leaseId"`
		WorkerID         uuid.UUID         `json:"workerId"`
		AttemptNumber    int               `json:"attemptNumber"`
		Operations       []json.RawMessage `json:"operations"`
		Source           requestedSource   `json:"source"`
	}
	if err := json.Unmarshal(encodedPayload, &payload); err != nil {
		t.Fatalf("decode acquired lease outbox payload: %v", err)
	}
	if payload.JobID != jobID || payload.DatasetVersionID != versionID || payload.AttemptID != lease.AttemptID ||
		payload.LeaseID != lease.LeaseID || payload.WorkerID != lease.WorkerID || payload.AttemptNumber != lease.AttemptNumber {
		t.Fatalf("acquired lease fence was incomplete or mismatched: %+v", payload)
	}
	if len(payload.Operations) != 1 {
		t.Fatalf("acquired lease command did not include operations: %s", encodedPayload)
	}
	if payload.Source != (requestedSource{
		ObjectKey: "fixtures/" + versionID.String(), ContentType: "text/csv", Format: "CSV", SizeBytes: 4096,
	}) {
		t.Fatalf("acquired lease source was not authoritative: %+v", payload.Source)
	}

	var rawPayload map[string]json.RawMessage
	if err := json.Unmarshal(encodedPayload, &rawPayload); err != nil {
		t.Fatalf("decode raw acquired lease payload: %v", err)
	}
	var rawSource map[string]json.RawMessage
	if err := json.Unmarshal(rawPayload["source"], &rawSource); err != nil {
		t.Fatalf("decode acquired lease source: %v", err)
	}
	if len(rawSource) != 4 {
		t.Fatalf("acquired lease source contains unexpected fields: %v", rawSource)
	}
	if _, found := rawSource["originalFilename"]; found {
		t.Fatalf("acquired lease source leaked user-controlled filename: %v", rawSource)
	}
}

func assertFailedAcquireRolledBack(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID) {
	t.Helper()
	var jobState, attemptState string
	var leaseID, workerID *uuid.UUID
	if err := pool.QueryRow(ctx, `
SELECT j.state, a.state, a.lease_id, a.worker_id
FROM processing_jobs j
JOIN job_attempts a ON a.job_id = j.id
WHERE j.id = $1`, jobID).Scan(&jobState, &attemptState, &leaseID, &workerID); err != nil {
		t.Fatalf("select failed acquisition state: %v", err)
	}
	if jobState != "QUEUED" || attemptState != "CREATED" || leaseID != nil || workerID != nil {
		t.Fatalf("failed acquisition changed lease state: job=%s attempt=%s lease=%v worker=%v", jobState, attemptState, leaseID, workerID)
	}
	var commands int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_messages WHERE payload->>'jobId' = $1`, jobID.String()).Scan(&commands); err != nil {
		t.Fatalf("count failed acquisition outbox commands: %v", err)
	}
	if commands != 0 {
		t.Fatalf("failed acquisition left %d outbox commands", commands)
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
	if _, err := pool.Exec(ctx, `INSERT INTO dataset_versions (id, dataset_id, version_number, state, original_filename, content_type, format, size_bytes, object_key, artifact_prefix, created_by, available_at) VALUES ($1, $2, 1, 'AVAILABLE', 'fixture.csv', 'text/csv', 'CSV', 4096, $3, $4, $5, NOW())`, versionID, datasetID, "fixtures/"+versionID.String(), "artifacts/"+versionID.String(), ownerID); err != nil {
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

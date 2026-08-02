package lease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	LeaseDuration time.Duration
	RenewalWindow time.Duration
	SweepLimit    int
	RetryPolicy   retry.Policy
	Clock         Clock
	Random        retry.Source
}

func DefaultConfig() Config {
	return Config{
		LeaseDuration: DefaultLeaseDuration,
		RenewalWindow: DefaultRenewalWindow,
		SweepLimit:    DefaultSweepBatchLimit,
		RetryPolicy:   retry.DefaultPolicy(),
		Clock:         realClock{},
		Random:        newLockedRandom(),
	}
}

type Repository struct {
	pool   *pgxpool.Pool
	outbox outbox.Enqueuer
	config Config
}

func NewRepository(pool *pgxpool.Pool, writer outbox.Enqueuer, config Config) (*Repository, error) {
	if pool == nil || writer == nil {
		return nil, errors.New("lease repository requires database and outbox dependencies")
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = DefaultLeaseDuration
	}
	if config.RenewalWindow == 0 {
		config.RenewalWindow = DefaultRenewalWindow
	}
	if config.SweepLimit == 0 {
		config.SweepLimit = DefaultSweepBatchLimit
	}
	if config.RetryPolicy.MaxAttempts == 0 {
		config.RetryPolicy = retry.DefaultPolicy()
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	if config.Random == nil {
		config.Random = newLockedRandom()
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	return &Repository{pool: pool, outbox: writer, config: config}, nil
}

func (r *Repository) Acquire(ctx context.Context, command AcquireCommand) (Lease, error) {
	if r == nil || r.pool == nil || r.outbox == nil {
		return Lease{}, errors.New("lease repository is not configured")
	}
	if command.JobID == uuid.Nil || command.WorkerID == uuid.Nil {
		return Lease{}, fmt.Errorf("%w: job and worker are required", ErrInvalidInput)
	}
	duration, err := normalizeDuration(command.Duration, r.config.LeaseDuration)
	if err != nil {
		return Lease{}, err
	}
	now := r.config.Clock.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Lease{}, fmt.Errorf("begin lease acquisition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var item leaseCandidate
	err = tx.QueryRow(ctx, `
SELECT j.id, j.state, j.next_attempt_at, j.max_attempts, j.dataset_version_id,
       a.id, a.attempt_number, a.state
FROM processing_jobs j
JOIN job_attempts a ON a.job_id = j.id
WHERE j.id = $1
  AND a.attempt_number = (SELECT MAX(attempt_number) FROM job_attempts WHERE job_id = j.id)
	FOR UPDATE OF j, a`, command.JobID).Scan(
		&item.JobID, &item.JobState, &item.NextAttemptAt, &item.MaxAttempts, &item.DatasetVersionID,
		&item.AttemptID, &item.AttemptNumber, &item.AttemptState,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, job.ErrJobNotFound
	}
	if err != nil {
		return Lease{}, fmt.Errorf("lock job for lease acquisition: %w", err)
	}
	if item.JobState != job.StateQueued || item.AttemptState != job.AttemptCreated {
		return Lease{}, fmt.Errorf("%w: job or attempt is not queued", job.ErrJobState)
	}
	if item.NextAttemptAt != nil && item.NextAttemptAt.After(now) {
		return Lease{}, ErrLeaseNotReady
	}
	leaseID := uuid.New()
	expiresAt := now.Add(duration)
	if _, err := tx.Exec(ctx, `
UPDATE job_attempts
SET state = $2, lease_id = $3, worker_id = $4, leased_at = $5, last_renewed_at = $5, lease_expires_at = $6
WHERE id = $1`, item.AttemptID, job.AttemptLeased, leaseID, command.WorkerID, now, expiresAt); err != nil {
		return Lease{}, fmt.Errorf("assign job lease: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, next_attempt_at = NULL, updated_at = $3
WHERE id = $1`, item.JobID, job.StateLeased, now); err != nil {
		return Lease{}, fmt.Errorf("mark job leased: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE job_quota_counters
SET queued_count = GREATEST(queued_count - 1, 0), active_count = active_count + 1, updated_at = $2
WHERE owner_user_id = (SELECT owner_user_id FROM processing_jobs WHERE id = $1)`, item.JobID, now); err != nil {
		return Lease{}, fmt.Errorf("update lease quota: %w", err)
	}
	if err := recordHistory(ctx, tx, item.JobID, job.StateQueued, job.StateLeased, "lease_assigned"); err != nil {
		return Lease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, fmt.Errorf("commit lease acquisition: %w", err)
	}
	return Lease{LeaseID: leaseID, JobID: item.JobID, AttemptID: item.AttemptID, WorkerID: command.WorkerID, AttemptNumber: item.AttemptNumber, LeasedAt: now, LeaseExpiresAt: expiresAt, LastRenewedAt: now}, nil
}

func (r *Repository) Renew(ctx context.Context, command RenewCommand) (Lease, error) {
	if r == nil || r.pool == nil {
		return Lease{}, errors.New("lease repository is not configured")
	}
	if command.LeaseID == uuid.Nil || command.WorkerID == uuid.Nil {
		return Lease{}, fmt.Errorf("%w: lease and worker are required", ErrInvalidInput)
	}
	duration, err := normalizeDuration(command.Duration, r.config.RenewalWindow)
	if err != nil {
		return Lease{}, err
	}
	now := r.config.Clock.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Lease{}, fmt.Errorf("begin lease renewal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item Lease
	var state string
	var expiresAt *time.Time
	var leasedAt, lastRenewedAt *time.Time
	err = tx.QueryRow(ctx, `
SELECT a.lease_id, a.job_id, a.id, a.worker_id, a.attempt_number, a.state, a.leased_at, a.last_renewed_at, a.lease_expires_at
FROM job_attempts a
WHERE a.lease_id = $1 AND a.worker_id = $2
FOR UPDATE`, command.LeaseID, command.WorkerID).Scan(&item.LeaseID, &item.JobID, &item.AttemptID, &item.WorkerID, &item.AttemptNumber, &state, &leasedAt, &lastRenewedAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, ErrLeaseNotFound
	}
	if err != nil {
		return Lease{}, fmt.Errorf("lock lease for renewal: %w", err)
	}
	if state != job.AttemptLeased && state != job.AttemptRunning {
		return Lease{}, ErrLeaseState
	}
	if expiresAt == nil || !expiresAt.After(now) {
		return Lease{}, ErrLeaseExpired
	}
	if leasedAt != nil {
		item.LeasedAt = *leasedAt
	}
	if lastRenewedAt != nil {
		item.LastRenewedAt = *lastRenewedAt
	}
	item.LastRenewedAt = now
	item.LeaseExpiresAt = now.Add(duration)
	if _, err := tx.Exec(ctx, `
UPDATE job_attempts SET last_renewed_at = $2, lease_expires_at = $3
WHERE lease_id = $1 AND worker_id = $4`, command.LeaseID, now, item.LeaseExpiresAt, command.WorkerID); err != nil {
		return Lease{}, fmt.Errorf("renew job lease: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, fmt.Errorf("commit lease renewal: %w", err)
	}
	return item, nil
}

type leaseCandidate struct {
	JobID            uuid.UUID
	JobState         string
	NextAttemptAt    *time.Time
	MaxAttempts      int16
	DatasetVersionID uuid.UUID
	AttemptID        uuid.UUID
	AttemptNumber    int
	AttemptState     string
}

type expiredCandidate struct {
	JobID, AttemptID, LeaseID, WorkerID, DatasetVersionID, OwnerUserID uuid.UUID
	JobState, AttemptState                                             string
	AttemptNumber, MaxAttempts                                         int
	Operations                                                         []byte
}

func (r *Repository) SweepExpired(ctx context.Context, limit int) (SweepReport, error) {
	if r == nil || r.pool == nil || r.outbox == nil {
		return SweepReport{}, errors.New("lease repository is not configured")
	}
	if limit == 0 {
		limit = r.config.SweepLimit
	}
	if limit < 1 || limit > MaxSweepBatchLimit {
		return SweepReport{}, fmt.Errorf("%w: sweep limit must be between 1 and %d", ErrInvalidInput, MaxSweepBatchLimit)
	}
	now := r.config.Clock.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SweepReport{}, fmt.Errorf("begin expired lease sweep: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT j.id, j.state, j.owner_user_id, j.dataset_version_id, j.max_attempts, j.operations,
       a.id, a.attempt_number, a.state, a.lease_id, a.worker_id
FROM processing_jobs j
JOIN job_attempts a ON a.job_id = j.id
WHERE j.state IN ('LEASED', 'RUNNING')
  AND a.state IN ('LEASED', 'RUNNING')
  AND a.lease_expires_at IS NOT NULL
  AND a.lease_expires_at <= $1
ORDER BY a.lease_expires_at, a.id
FOR UPDATE OF j, a SKIP LOCKED
LIMIT $2`, now, limit)
	if err != nil {
		return SweepReport{}, fmt.Errorf("select expired leases: %w", err)
	}
	defer rows.Close()
	candidates := make([]expiredCandidate, 0, limit)
	for rows.Next() {
		var item expiredCandidate
		if err := rows.Scan(&item.JobID, &item.JobState, &item.OwnerUserID, &item.DatasetVersionID, &item.MaxAttempts, &item.Operations, &item.AttemptID, &item.AttemptNumber, &item.AttemptState, &item.LeaseID, &item.WorkerID); err != nil {
			return SweepReport{}, fmt.Errorf("scan expired lease: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return SweepReport{}, fmt.Errorf("iterate expired leases: %w", err)
	}
	report := SweepReport{Claimed: len(candidates)}
	for _, item := range candidates {
		if err := r.expireOne(ctx, tx, item, now, &report); err != nil {
			return SweepReport{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SweepReport{}, fmt.Errorf("commit expired lease sweep: %w", err)
	}
	return report, nil
}

func (r *Repository) expireOne(ctx context.Context, tx pgx.Tx, item expiredCandidate, now time.Time, report *SweepReport) error {
	if _, err := tx.Exec(ctx, `UPDATE job_attempts SET state = $2, finished_at = $3, lease_expires_at = $3 WHERE id = $1`, item.AttemptID, job.AttemptTimedOut, now); err != nil {
		return fmt.Errorf("time out attempt: %w", err)
	}
	nextAttempt := item.AttemptNumber + 1
	canRetry := item.AttemptNumber >= 1 && nextAttempt <= item.MaxAttempts
	if canRetry {
		policy := r.config.RetryPolicy
		if policy.MaxAttempts < item.MaxAttempts {
			policy.MaxAttempts = item.MaxAttempts
		}
		delay, err := policy.Delay(nextAttempt, r.config.Random)
		if err != nil {
			return err
		}
		nextAt := now.Add(delay)
		newAttemptID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO job_attempts (id, job_id, attempt_number, state, created_at) VALUES ($1, $2, $3, $4, $5)`, newAttemptID, item.JobID, nextAttempt, job.AttemptCreated, now); err != nil {
			return fmt.Errorf("create retry attempt: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, next_attempt_at = $3, updated_at = $4, last_error_code = 'LEASE_EXPIRED', last_error_message = 'worker lease expired'
WHERE id = $1`, item.JobID, job.StateQueued, nextAt, now); err != nil {
			return fmt.Errorf("queue expired job retry: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = GREATEST(active_count - 1, 0), queued_count = queued_count + 1, updated_at = $2 WHERE owner_user_id = $1`, item.OwnerUserID, now); err != nil {
			return fmt.Errorf("update retry quota: %w", err)
		}
		if err := recordHistory(ctx, tx, item.JobID, item.JobState, job.StateQueued, "lease_expired_retry_scheduled"); err != nil {
			return err
		}
		if err := enqueueRetry(ctx, tx, r.outbox, item, nextAt); err != nil {
			return err
		}
		report.Retried++
		return nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE processing_jobs
SET state = $2, next_attempt_at = NULL, updated_at = $3, finished_at = $3,
    last_error_code = 'LEASE_EXPIRED', last_error_message = 'worker lease expired'
WHERE id = $1`, item.JobID, job.StateDeadLettered, now); err != nil {
		return fmt.Errorf("dead-letter expired job: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE job_quota_counters SET active_count = GREATEST(active_count - 1, 0), updated_at = $2 WHERE owner_user_id = $1`, item.OwnerUserID, now); err != nil {
		return fmt.Errorf("update dead-letter quota: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_dead_letters (id, job_id, attempt_id, lease_id, worker_id, attempt_number, error_code, error_message, retryable)
VALUES ($1, $2, $3, $4, $5, $6, 'LEASE_EXPIRED', 'worker lease expired', TRUE)
ON CONFLICT (job_id, attempt_id) DO NOTHING`, uuid.New(), item.JobID, item.AttemptID, item.LeaseID, item.WorkerID, item.AttemptNumber); err != nil {
		return fmt.Errorf("persist expired job dead-letter: %w", err)
	}
	if err := recordHistory(ctx, tx, item.JobID, item.JobState, job.StateDeadLettered, "lease_expired_dead_lettered"); err != nil {
		return err
	}
	report.DeadLettered++
	return nil
}

func enqueueRetry(ctx context.Context, tx pgx.Tx, writer outbox.Enqueuer, item expiredCandidate, availableAt time.Time) error {
	var operations []job.Operation
	if err := json.Unmarshal(item.Operations, &operations); err != nil {
		return fmt.Errorf("decode retry operations: %w", err)
	}
	traceID := item.JobID.String()
	envelope, err := queue.NewEnvelope(queue.MessageJobRequested, traceID, traceID, item.LeaseID.String(), struct {
		JobID            uuid.UUID       `json:"jobId"`
		DatasetVersionID uuid.UUID       `json:"datasetVersionId"`
		Operations       []job.Operation `json:"operations"`
	}{JobID: item.JobID, DatasetVersionID: item.DatasetVersionID, Operations: operations})
	if err != nil {
		return fmt.Errorf("create retry command: %w", err)
	}
	return writer.Enqueue(ctx, tx, outbox.Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType, AvailableAt: availableAt})
}

func recordHistory(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, fromState, toState, reason string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO job_state_history (job_id, from_state, to_state, reason, actor_user_id) VALUES ($1, NULLIF($2, ''), $3, $4, NULL)`, jobID, fromState, toState, reason); err != nil {
		return fmt.Errorf("record lease state history: %w", err)
	}
	return nil
}

func normalizeDuration(value, fallback time.Duration) (time.Duration, error) {
	if value == 0 {
		value = fallback
	}
	if value <= 0 || value > MaxLeaseDuration {
		return 0, fmt.Errorf("%w: lease duration must be between 1ns and %s", ErrInvalidInput, MaxLeaseDuration)
	}
	return value, nil
}

func validateConfig(config Config) error {
	if _, err := normalizeDuration(config.LeaseDuration, 0); err != nil {
		return err
	}
	if _, err := normalizeDuration(config.RenewalWindow, 0); err != nil {
		return err
	}
	if config.SweepLimit < 1 || config.SweepLimit > MaxSweepBatchLimit {
		return fmt.Errorf("%w: sweep limit must be between 1 and %d", ErrInvalidInput, MaxSweepBatchLimit)
	}
	return config.RetryPolicy.Validate()
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type lockedRandom struct {
	mu     sync.Mutex
	random *rand.Rand
}

func newLockedRandom() *lockedRandom {
	return &lockedRandom{random: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (r *lockedRandom) Int63n(maximum int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.random.Int63n(maximum)
}

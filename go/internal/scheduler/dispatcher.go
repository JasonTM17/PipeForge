package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/lease"
	"github.com/google/uuid"
)

// LeaseAcquirer is the transactional boundary that allocates a worker lease
// and materializes the fence-complete execution command in the same commit.
type LeaseAcquirer interface {
	Acquire(context.Context, lease.AcquireCommand) (lease.Lease, error)
}

type JobSelector interface {
	Select(context.Context) ([]job.QueuedJob, error)
}

type DispatchConfig struct {
	WorkerID      uuid.UUID
	LeaseDuration time.Duration
}

// DispatchReport distinguishes contention and not-ready jobs from real
// failures, so a single stale broker delivery never stops reconciliation.
type DispatchReport struct {
	Selected int
	Acquired int
	Skipped  int
	Failures []error
}

// Dispatcher joins fair selection with lease acquisition. It intentionally
// does not publish to RabbitMQ: publication is handled by the durable outbox.
type Dispatcher struct {
	Selector JobSelector
	Acquirer LeaseAcquirer
	Config   DispatchConfig
}

func NewDispatcher(selector JobSelector, acquirer LeaseAcquirer, config DispatchConfig) (*Dispatcher, error) {
	if selector == nil || acquirer == nil || config.WorkerID == uuid.Nil || config.LeaseDuration <= 0 {
		return nil, errors.New("scheduler dispatcher requires selector, acquirer, worker ID, and lease duration")
	}
	return &Dispatcher{Selector: selector, Acquirer: acquirer, Config: config}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context) (DispatchReport, error) {
	if d == nil || d.Selector == nil || d.Acquirer == nil || d.Config.WorkerID == uuid.Nil || d.Config.LeaseDuration <= 0 {
		return DispatchReport{}, errors.New("scheduler dispatcher is not configured")
	}
	queued, err := d.Selector.Select(ctx)
	if err != nil {
		return DispatchReport{}, fmt.Errorf("select queued jobs: %w", err)
	}
	report := DispatchReport{Selected: len(queued), Failures: make([]error, 0)}
	for _, item := range queued {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		_, acquireErr := d.Acquirer.Acquire(ctx, lease.AcquireCommand{
			JobID: item.ID, WorkerID: d.Config.WorkerID, Duration: d.Config.LeaseDuration,
		})
		if acquireErr == nil {
			report.Acquired++
			continue
		}
		if errors.Is(acquireErr, lease.ErrLeaseNotReady) || errors.Is(acquireErr, job.ErrJobNotFound) || errors.Is(acquireErr, job.ErrJobState) {
			report.Skipped++
			continue
		}
		report.Failures = append(report.Failures, fmt.Errorf("acquire job %s: %w", item.ID, acquireErr))
	}
	return report, nil
}

package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/lease"
	"github.com/google/uuid"
)

type selectorStub struct {
	items []job.QueuedJob
	err   error
}

func (s selectorStub) Select(context.Context) ([]job.QueuedJob, error) { return s.items, s.err }

type acquirerStub struct {
	commands []lease.AcquireCommand
	errors   map[uuid.UUID]error
}

func (a *acquirerStub) Acquire(_ context.Context, command lease.AcquireCommand) (lease.Lease, error) {
	a.commands = append(a.commands, command)
	if err := a.errors[command.JobID]; err != nil {
		return lease.Lease{}, err
	}
	return lease.Lease{JobID: command.JobID}, nil
}

func TestDispatcherAcquiresEachFairlySelectedJob(t *testing.T) {
	firstJobID, secondJobID := uuid.New(), uuid.New()
	acquirer := &acquirerStub{}
	dispatcher, err := NewDispatcher(selectorStub{items: []job.QueuedJob{{Job: job.Job{ID: firstJobID}}, {Job: job.Job{ID: secondJobID}}}}, acquirer, DispatchConfig{LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("NewDispatcher returned error: %v", err)
	}

	report, err := dispatcher.Dispatch(context.Background())
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if report.Selected != 2 || report.Acquired != 2 || report.Skipped != 0 || len(report.Failures) != 0 {
		t.Fatalf("unexpected dispatch report: %+v", report)
	}
	if len(acquirer.commands) != 2 || acquirer.commands[0].JobID != firstJobID || acquirer.commands[1].JobID != secondJobID {
		t.Fatalf("unexpected acquired jobs: %+v", acquirer.commands)
	}
	for _, command := range acquirer.commands {
		if command.Duration != time.Minute {
			t.Fatalf("dispatcher did not preserve configured lease duration: %+v", command)
		}
	}
}

func TestDispatcherTreatsDuplicateOrNotReadyJobsAsSafeSkips(t *testing.T) {
	firstJobID, secondJobID := uuid.New(), uuid.New()
	acquirer := &acquirerStub{errors: map[uuid.UUID]error{firstJobID: job.ErrJobState, secondJobID: lease.ErrLeaseNotReady}}
	dispatcher, err := NewDispatcher(selectorStub{items: []job.QueuedJob{{Job: job.Job{ID: firstJobID}}, {Job: job.Job{ID: secondJobID}}}}, acquirer, DispatchConfig{LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("NewDispatcher returned error: %v", err)
	}

	report, err := dispatcher.Dispatch(context.Background())
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if report.Selected != 2 || report.Acquired != 0 || report.Skipped != 2 || len(report.Failures) != 0 {
		t.Fatalf("unexpected safe-skip report: %+v", report)
	}
}

func TestDispatcherReportsIndividualAcquisitionFailureAndContinues(t *testing.T) {
	firstJobID, secondJobID := uuid.New(), uuid.New()
	acquirer := &acquirerStub{errors: map[uuid.UUID]error{firstJobID: errors.New("database unavailable")}}
	dispatcher, err := NewDispatcher(selectorStub{items: []job.QueuedJob{{Job: job.Job{ID: firstJobID}}, {Job: job.Job{ID: secondJobID}}}}, acquirer, DispatchConfig{LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("NewDispatcher returned error: %v", err)
	}

	report, err := dispatcher.Dispatch(context.Background())
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if report.Acquired != 1 || report.Skipped != 0 || len(report.Failures) != 1 || len(acquirer.commands) != 2 {
		t.Fatalf("unexpected continued dispatch report: %+v", report)
	}
}

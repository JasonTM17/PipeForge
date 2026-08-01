package lease

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type fakeLeaseStore struct{}

func (fakeLeaseStore) Acquire(context.Context, AcquireCommand) (Lease, error) {
	return Lease{}, nil
}

func (fakeLeaseStore) Renew(context.Context, RenewCommand) (Lease, error) {
	return Lease{}, nil
}

func (fakeLeaseStore) SweepExpired(context.Context, int) (SweepReport, error) {
	return SweepReport{}, nil
}

func TestServiceDelegatesValidatedLeaseCommands(t *testing.T) {
	service, err := NewService(fakeLeaseStore{})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	if _, err := service.Acquire(context.Background(), AcquireCommand{JobID: uuid.New(), WorkerID: uuid.New()}); err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if _, err := service.Renew(context.Background(), RenewCommand{LeaseID: uuid.New(), WorkerID: uuid.New()}); err != nil {
		t.Fatalf("Renew returned error: %v", err)
	}
}

func TestServiceRejectsMissingStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Fatal("nil store was accepted")
	}
	var service *Service
	if _, err := service.Acquire(context.Background(), AcquireCommand{}); err == nil {
		t.Fatal("unconfigured service was accepted")
	}
}

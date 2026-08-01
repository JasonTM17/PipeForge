package scheduler

import (
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/google/uuid"
)

func TestFairSelectRotatesOwnersBeforeTakingNoisyBacklog(t *testing.T) {
	noisyOwner, quietOwner := uuid.New(), uuid.New()
	created := time.Unix(100, 0)
	candidates := make([]job.QueuedJob, 0, 5)
	for index := 0; index < 4; index++ {
		candidates = append(candidates, job.QueuedJob{Job: job.Job{ID: uuid.New(), OwnerUserID: noisyOwner, State: job.StateQueued, Priority: 100, CreatedAt: created.Add(time.Duration(index) * time.Second)}})
	}
	candidates = append(candidates, job.QueuedJob{Job: job.Job{ID: uuid.New(), OwnerUserID: quietOwner, State: job.StateQueued, Priority: 0, CreatedAt: created.Add(-time.Hour)}})
	selected := FairSelect(candidates, 2)
	if len(selected) != 2 {
		t.Fatalf("expected two selected jobs, got %d", len(selected))
	}
	if selected[0].OwnerUserID == selected[1].OwnerUserID {
		t.Fatalf("fair selection chose both jobs from owner %s", selected[0].OwnerUserID)
	}
}

func TestFairSelectKeepsPriorityOrderWithinOwner(t *testing.T) {
	owner := uuid.New()
	candidates := []job.QueuedJob{
		{Job: job.Job{ID: uuid.New(), OwnerUserID: owner, State: job.StateQueued, Priority: 1, CreatedAt: time.Unix(1, 0)}},
		{Job: job.Job{ID: uuid.New(), OwnerUserID: owner, State: job.StateQueued, Priority: 10, CreatedAt: time.Unix(2, 0)}},
	}
	selected := FairSelect(candidates, 2)
	if selected[0].Priority != 10 || selected[1].Priority != 1 {
		t.Fatalf("priority order was not preserved within owner: %+v", selected)
	}
}

package scheduler

import (
	"context"
	"sort"

	"github.com/JasonTM17/PipeForge/go/internal/job"
)

const (
	DefaultBatchSize       = 20
	DefaultCandidateFactor = 4
)

type Config struct {
	BatchSize       int
	CandidateFactor int
}

type Scheduler struct {
	Store  job.QueueStore
	Config Config
}

func New(store job.QueueStore, config Config) *Scheduler {
	if config.BatchSize < 1 {
		config.BatchSize = DefaultBatchSize
	}
	if config.CandidateFactor < 2 {
		config.CandidateFactor = DefaultCandidateFactor
	}
	return &Scheduler{Store: store, Config: config}
}

func (s *Scheduler) Select(ctx context.Context) ([]job.QueuedJob, error) {
	if s == nil || s.Store == nil {
		return nil, job.ErrInvalidInput
	}
	limit := s.Config.BatchSize * s.Config.CandidateFactor
	if limit > 1000 {
		limit = 1000
	}
	candidates, err := s.Store.SelectQueued(ctx, job.QueueQuery{Limit: limit})
	if err != nil {
		return nil, err
	}
	return FairSelect(candidates, s.Config.BatchSize), nil
}

func FairSelect(candidates []job.QueuedJob, limit int) []job.QueuedJob {
	if limit <= 0 || len(candidates) == 0 {
		return []job.QueuedJob{}
	}
	byOwner := make(map[string][]job.QueuedJob)
	for _, candidate := range candidates {
		owner := candidate.OwnerUserID.String()
		byOwner[owner] = append(byOwner[owner], candidate)
	}
	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
		sort.SliceStable(byOwner[owner], func(left, right int) bool {
			return candidateBefore(byOwner[owner][left], byOwner[owner][right])
		})
	}
	sort.Slice(owners, func(left, right int) bool {
		return candidateBefore(byOwner[owners[left]][0], byOwner[owners[right]][0])
	})
	selected := make([]job.QueuedJob, 0, min(limit, len(candidates)))
	for round := 0; len(selected) < limit; round++ {
		added := false
		for _, owner := range owners {
			items := byOwner[owner]
			if round >= len(items) {
				continue
			}
			selected = append(selected, items[round])
			added = true
			if len(selected) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return selected
}

func candidateBefore(left, right job.QueuedJob) bool {
	if left.Priority != right.Priority {
		return left.Priority > right.Priority
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	return left.ID.String() < right.ID.String()
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

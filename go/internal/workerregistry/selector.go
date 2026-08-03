package workerregistry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) SelectForLease(ctx context.Context, tx pgx.Tx, operations []string, now time.Time) (uuid.UUID, bool, error) {
	if r == nil || tx == nil || len(operations) == 0 {
		return uuid.Nil, false, errors.New("worker selection requires a registry transaction and operations")
	}
	required, err := normalizedRequiredOperations(operations)
	if err != nil {
		return uuid.Nil, false, err
	}
	cutoff := now.UTC().Add(-r.config.HeartbeatTTL)
	excluded := make([]uuid.UUID, 0)
	for {
		var workerID uuid.UUID
		var maxConcurrency int
		err = tx.QueryRow(ctx, `
SELECT w.worker_id, w.max_concurrency
FROM processing_workers w
WHERE w.status IN ('READY', 'BUSY')
  AND w.last_heartbeat_at IS NOT NULL
  AND w.last_heartbeat_at >= $1
  AND w.supported_operations @> $2::text[]
  AND NOT (w.worker_id = ANY($3::uuid[]))
ORDER BY w.last_assigned_at NULLS FIRST, w.worker_id
FOR UPDATE OF w SKIP LOCKED
LIMIT 1`, cutoff, required, excluded).Scan(&workerID, &maxConcurrency)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("lock eligible worker: %w", err)
		}

		var activeLeases int
		if err := tx.QueryRow(ctx, `
SELECT COUNT(*)
FROM job_attempts
WHERE worker_id = $1
  AND state IN ('LEASED', 'RUNNING')`, workerID).Scan(&activeLeases); err != nil {
			return uuid.Nil, false, fmt.Errorf("count active worker leases: %w", err)
		}
		if activeLeases >= maxConcurrency {
			excluded = append(excluded, workerID)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE processing_workers SET last_assigned_at = $2, updated_at = $2 WHERE worker_id = $1`, workerID, now.UTC()); err != nil {
			return uuid.Nil, false, fmt.Errorf("mark worker assignment: %w", err)
		}
		return workerID, true, nil
	}
}

func normalizedRequiredOperations(operations []string) ([]string, error) {
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		normalized := strings.TrimSpace(operation)
		if !operationPattern.MatchString(normalized) {
			return nil, fmt.Errorf("required operation %q is invalid", operation)
		}
		seen[normalized] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for operation := range seen {
		result = append(result, operation)
	}
	sort.Strings(result)
	return result, nil
}

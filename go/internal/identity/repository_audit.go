package identity

import (
	"context"
	"encoding/json"
	"fmt"
)

func (r *Repository) RecordAudit(ctx context.Context, event AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO audit_events (actor_user_id, action, resource_type, resource_id, request_id, metadata)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6::jsonb)`, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, event.RequestID, metadata)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

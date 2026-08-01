package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) Create(ctx context.Context, ownerID, actorID uuid.UUID, request CreateRequest) (Rule, error) {
	configuration, err := json.Marshal(request.Configuration)
	if err != nil {
		return Rule{}, fmt.Errorf("marshal quality configuration: %w", err)
	}
	id := uuid.New()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Rule{}, fmt.Errorf("begin quality rule creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rule, err := scanQualityRule(tx.QueryRow(ctx, `
INSERT INTO quality_rules (id, owner_user_id, dataset_id, name, column_scope, rule_type, configuration, severity, enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
RETURNING id, owner_user_id, dataset_id, name, column_scope, rule_type, configuration, severity, enabled, created_by, created_at, updated_at, deleted_at`,
		id, ownerID, request.DatasetID, request.Name, request.ColumnScope, string(request.RuleType), configuration, string(request.Severity), enabledValue(request.Enabled), actorID))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Rule{}, ErrRuleNameUsed
		}
		return Rule{}, fmt.Errorf("insert quality rule: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_events (actor_user_id, action, resource_type, resource_id, metadata)
VALUES ($1, 'quality_rule.created', 'quality_rule', $2, jsonb_build_object('datasetId', $3::text))`, actorID, id.String(), request.DatasetID); err != nil {
		return Rule{}, fmt.Errorf("audit quality rule creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("commit quality rule creation: %w", err)
	}
	return rule, nil
}

func (r *Repository) List(ctx context.Context, query ListQuery) (Page, error) {
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return Page{}, fmt.Errorf("%w: page and pageSize are invalid", ErrInvalidInput)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM quality_rules
WHERE deleted_at IS NULL
  AND ($1::uuid IS NULL OR owner_user_id = $1)
  AND ($2::uuid IS NULL OR dataset_id = $2)`, query.OwnerUserID, query.DatasetID).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count quality rules: %w", err)
	}
	rows, err := r.pool.Query(ctx, qualityRuleSelect+`WHERE qr.deleted_at IS NULL
  AND ($1::uuid IS NULL OR qr.owner_user_id = $1)
  AND ($2::uuid IS NULL OR qr.dataset_id = $2)
ORDER BY qr.created_at DESC, qr.id
LIMIT $3 OFFSET $4`, query.OwnerUserID, query.DatasetID, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return Page{}, fmt.Errorf("list quality rules: %w", err)
	}
	defer rows.Close()
	items := make([]Rule, 0, query.PageSize)
	for rows.Next() {
		item, err := scanQualityRule(rows)
		if err != nil {
			return Page{}, fmt.Errorf("scan quality rule: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate quality rules: %w", err)
	}
	return Page{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, nil
}

func (r *Repository) Get(ctx context.Context, ruleID uuid.UUID) (Rule, error) {
	rule, err := scanQualityRule(r.pool.QueryRow(ctx, qualityRuleSelect+`WHERE qr.id = $1 AND qr.deleted_at IS NULL`, ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, ErrRuleNotFound
	}
	if err != nil {
		return Rule{}, fmt.Errorf("get quality rule: %w", err)
	}
	return rule, nil
}

func (r *Repository) Update(ctx context.Context, ruleID, actorID uuid.UUID, request UpdateRequest) (Rule, error) {
	configuration, err := optionalConfiguration(request.Configuration)
	if err != nil {
		return Rule{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Rule{}, fmt.Errorf("begin quality rule update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rule, err := scanQualityRule(tx.QueryRow(ctx, `
UPDATE quality_rules
SET name = COALESCE($1, name), column_scope = COALESCE($2, column_scope),
    rule_type = COALESCE($3, rule_type), configuration = COALESCE($4::jsonb, configuration),
    severity = COALESCE($5, severity), enabled = COALESCE($6, enabled), updated_at = NOW()
WHERE id = $7 AND deleted_at IS NULL
RETURNING id, owner_user_id, dataset_id, name, column_scope, rule_type, configuration, severity, enabled, created_by, created_at, updated_at, deleted_at`,
		normalizedName(request.Name), request.ColumnScope, optionalRuleType(request.RuleType), configuration,
		optionalSeverity(request.Severity), optionalBool(request.Enabled), ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, ErrRuleNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Rule{}, ErrRuleNameUsed
		}
		return Rule{}, fmt.Errorf("update quality rule: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_events (actor_user_id, action, resource_type, resource_id)
VALUES ($1, 'quality_rule.updated', 'quality_rule', $2)`, actorID, ruleID.String()); err != nil {
		return Rule{}, fmt.Errorf("audit quality rule update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("commit quality rule update: %w", err)
	}
	return rule, nil
}

func (r *Repository) Delete(ctx context.Context, ruleID, actorID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin quality rule deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `UPDATE quality_rules SET deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, ruleID)
	if err != nil {
		return fmt.Errorf("delete quality rule: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_events (actor_user_id, action, resource_type, resource_id)
VALUES ($1, 'quality_rule.deleted', 'quality_rule', $2)`, actorID, ruleID.String()); err != nil {
		return fmt.Errorf("audit quality rule deletion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit quality rule deletion: %w", err)
	}
	return nil
}

const qualityRuleSelect = `
SELECT qr.id, qr.owner_user_id, qr.dataset_id, qr.name, qr.column_scope, qr.rule_type,
       qr.configuration, qr.severity, qr.enabled, qr.created_by, qr.created_at, qr.updated_at, qr.deleted_at
FROM quality_rules qr `

type qualityRuleRow interface {
	Scan(...any) error
}

func scanQualityRule(row qualityRuleRow) (Rule, error) {
	var rule Rule
	var configuration []byte
	if err := row.Scan(&rule.ID, &rule.OwnerUserID, &rule.DatasetID, &rule.Name, &rule.ColumnScope, &rule.RuleType, &configuration, &rule.Severity, &rule.Enabled, &rule.CreatedBy, &rule.CreatedAt, &rule.UpdatedAt, &rule.DeletedAt); err != nil {
		return Rule{}, err
	}
	if len(configuration) == 0 {
		rule.Configuration = map[string]any{}
	} else if err := json.Unmarshal(configuration, &rule.Configuration); err != nil {
		return Rule{}, fmt.Errorf("decode quality configuration: %w", err)
	}
	return rule, nil
}

func enabledValue(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func optionalConfiguration(value *map[string]any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(*value)
	if err != nil {
		return nil, fmt.Errorf("marshal quality configuration: %w", err)
	}
	return encoded, nil
}

func optionalBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func normalizedName(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalRuleType(value *RuleType) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func optionalSeverity(value *Severity) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

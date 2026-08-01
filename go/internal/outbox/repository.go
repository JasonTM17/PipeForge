package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("outbox repository requires a database pool")
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Enqueue(ctx context.Context, tx pgx.Tx, message Message) error {
	if tx == nil {
		return errors.New("outbox enqueue requires a transaction")
	}
	if err := message.Validate(); err != nil {
		return err
	}
	availableAt := message.AvailableAt
	if availableAt.IsZero() {
		availableAt = time.Now().UTC()
	}
	headers, err := EncodeHeaders(nil)
	if err != nil {
		return fmt.Errorf("encode outbox headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
    id, message_id, message_type, schema_version, exchange, routing_key,
    occurred_at, trace_id, correlation_id, causation_id, payload, headers, available_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		message.ID, message.Envelope.MessageID, message.Envelope.MessageType, message.Envelope.SchemaVersion,
		message.Exchange, message.RoutingKey, message.Envelope.OccurredAt, message.Envelope.TraceID,
		message.Envelope.CorrelationID, message.Envelope.CausationID, []byte(message.Envelope.Payload), headers, availableAt)
	if err != nil {
		return fmt.Errorf("insert outbox message: %w", err)
	}
	return nil
}

func (r *Repository) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Message, error) {
	if lease <= 0 || limit < 1 || limit > 1000 {
		return nil, errors.New("outbox lease and batch limit are invalid")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
SELECT id, message_id, message_type, schema_version, exchange, routing_key,
       occurred_at, trace_id, correlation_id, causation_id, payload,
       attempts, available_at, lease_until, last_error
FROM outbox_messages
WHERE (state = 'PENDING' AND available_at <= $1)
   OR (state = 'IN_FLIGHT' AND lease_until <= $1)
ORDER BY available_at, created_at, id
FOR UPDATE SKIP LOCKED
LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("select outbox messages: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0, limit)
	for rows.Next() {
		message, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox messages: %w", err)
	}
	rows.Close()
	for index := range messages {
		leaseToken := uuid.New()
		leaseUntil := now.Add(lease)
		result, updateErr := tx.Exec(ctx, `
UPDATE outbox_messages
SET state = 'IN_FLIGHT', attempts = attempts + 1, lease_token = $2,
    lease_until = $3, updated_at = $4
WHERE id = $1`, messages[index].ID, leaseToken, leaseUntil, now)
		if updateErr != nil {
			return nil, fmt.Errorf("lease outbox message %s: %w", messages[index].ID, updateErr)
		}
		if result.RowsAffected() != 1 {
			return nil, fmt.Errorf("lease outbox message %s: %w", messages[index].ID, ErrLeaseLost)
		}
		messages[index].Attempts++
		messages[index].LeaseToken = leaseToken
		messages[index].LeaseUntil = leaseUntil
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit outbox claim: %w", err)
	}
	return messages, nil
}

func (r *Repository) MarkPublished(ctx context.Context, messageID, leaseToken uuid.UUID, publishedAt time.Time) error {
	result, err := r.pool.Exec(ctx, `
UPDATE outbox_messages
SET state = 'PUBLISHED', published_at = $3, updated_at = $3,
    lease_token = NULL, lease_until = NULL, last_error = NULL
WHERE id = $1 AND state = 'IN_FLIGHT' AND lease_token = $2`, messageID, leaseToken, publishedAt)
	if err != nil {
		return fmt.Errorf("mark outbox message published: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) MarkRetry(ctx context.Context, messageID, leaseToken uuid.UUID, nextAttempt time.Time, lastError string, maxAttempts int) error {
	if maxAttempts < 1 {
		return errors.New("outbox max attempts must be positive")
	}
	result, err := r.pool.Exec(ctx, `
UPDATE outbox_messages
SET state = CASE WHEN attempts >= $5 THEN 'FAILED' ELSE 'PENDING' END,
    available_at = $3, updated_at = NOW(), last_error = $4,
    lease_token = NULL, lease_until = NULL
WHERE id = $1 AND state = 'IN_FLIGHT' AND lease_token = $2`, messageID, leaseToken, nextAttempt, truncateError(lastError), maxAttempts)
	if err != nil {
		return fmt.Errorf("schedule outbox retry: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var message Message
	var messageType, traceID, correlationID, causationID string
	var payload []byte
	var lastError *string
	var leaseUntil *time.Time
	if err := row.Scan(
		&message.ID, &message.Envelope.MessageID, &messageType, &message.Envelope.SchemaVersion,
		&message.Exchange, &message.RoutingKey, &message.Envelope.OccurredAt, &traceID,
		&correlationID, &causationID, &payload, &message.Attempts, &message.AvailableAt,
		&leaseUntil, &lastError,
	); err != nil {
		return Message{}, fmt.Errorf("scan outbox message: %w", err)
	}
	message.Envelope.MessageType = messageType
	message.Envelope.TraceID = traceID
	message.Envelope.CorrelationID = correlationID
	message.Envelope.CausationID = causationID
	message.Envelope.Payload = json.RawMessage(payload)
	if leaseUntil != nil {
		message.LeaseUntil = *leaseUntil
	}
	if lastError != nil {
		message.LastError = *lastError
	}
	if err := message.Validate(); err != nil {
		return Message{}, fmt.Errorf("validate stored outbox message: %w", err)
	}
	return message, nil
}

func truncateError(value string) string {
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

var _ Store = (*Repository)(nil)
var _ Enqueuer = (*Repository)(nil)

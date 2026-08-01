package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	StatePending   = "PENDING"
	StateInFlight  = "IN_FLIGHT"
	StatePublished = "PUBLISHED"
	StateFailed    = "FAILED"
	DefaultLease   = 30 * time.Second
	DefaultRetry   = time.Second
	MaxRetryDelay  = time.Hour
)

var (
	ErrLeaseLost      = errors.New("outbox message lease is no longer owned")
	ErrInvalidMessage = errors.New("outbox message is invalid")
)

type Message struct {
	ID          uuid.UUID
	Envelope    queue.Envelope
	Exchange    string
	RoutingKey  string
	Attempts    int
	LeaseToken  uuid.UUID
	LeaseUntil  time.Time
	AvailableAt time.Time
	LastError   string
}

type Store interface {
	Claim(context.Context, time.Time, time.Duration, int) ([]Message, error)
	MarkPublished(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	MarkRetry(context.Context, uuid.UUID, uuid.UUID, time.Time, string, int) error
}

type Enqueuer interface {
	Enqueue(context.Context, pgx.Tx, Message) error
}

func (m Message) Validate() error {
	if m.ID == uuid.Nil {
		return errors.Join(ErrInvalidMessage, errors.New("outbox ID is required"))
	}
	if m.ID != m.Envelope.MessageID {
		return errors.Join(ErrInvalidMessage, errors.New("outbox ID must match envelope message ID"))
	}
	if m.Exchange == "" || m.RoutingKey == "" {
		return errors.Join(ErrInvalidMessage, errors.New("outbox exchange and routing key are required"))
	}
	if err := m.Envelope.Validate(); err != nil {
		return errors.Join(ErrInvalidMessage, err)
	}
	return nil
}

func EncodeHeaders(headers map[string]string) ([]byte, error) {
	if headers == nil {
		headers = map[string]string{}
	}
	return json.Marshal(headers)
}

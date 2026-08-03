package lease

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type captureEnqueuer struct{ message outbox.Message }

func (e *captureEnqueuer) Enqueue(_ context.Context, _ pgx.Tx, message outbox.Message) error {
	e.message = message
	return nil
}

func TestEnqueueRetryCreatesQueuedSchedulerSignal(t *testing.T) {
	availableAt := time.Now().UTC().Truncate(time.Microsecond)
	item := expiredCandidate{JobID: uuid.New(), LeaseID: uuid.New()}
	writer := &captureEnqueuer{}

	if err := enqueueRetry(context.Background(), nil, writer, item, availableAt); err != nil {
		t.Fatalf("enqueueRetry returned error: %v", err)
	}
	if writer.message.Exchange != queue.CommandsExchange || writer.message.RoutingKey != queue.MessageJobQueued || writer.message.Envelope.MessageType != queue.MessageJobQueued {
		t.Fatalf("retry emitted executable job command instead of queued scheduler signal: %+v", writer.message)
	}
	if !writer.message.AvailableAt.Equal(availableAt) {
		t.Fatalf("retry availability changed: got %s want %s", writer.message.AvailableAt, availableAt)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(writer.message.Envelope.Payload, &payload); err != nil {
		t.Fatalf("decode queued retry payload: %v", err)
	}
	if len(payload) != 1 || string(payload["jobId"]) != `"`+item.JobID.String()+`"` {
		t.Fatalf("retry queued payload is not job-only: %s", writer.message.Envelope.Payload)
	}
}

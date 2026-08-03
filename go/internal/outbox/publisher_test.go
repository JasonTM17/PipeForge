package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
)

type fakeStore struct {
	messages        []Message
	claimed         int
	published       int
	retried         int
	poisoned        int
	lastNextAttempt time.Time
}

func (s *fakeStore) Claim(_ context.Context, now time.Time, lease time.Duration, limit int) ([]Message, error) {
	if s.claimed >= len(s.messages) || limit == 0 {
		return nil, nil
	}
	message := s.messages[s.claimed]
	s.claimed++
	message.LeaseToken = uuid.New()
	message.LeaseUntil = now.Add(lease)
	return []Message{message}, nil
}

func (s *fakeStore) MarkPublished(_ context.Context, _, _ uuid.UUID, _ time.Time) error {
	s.published++
	return nil
}

func (s *fakeStore) MarkRetry(_ context.Context, _, _ uuid.UUID, next time.Time, _ string, maxAttempts int) error {
	s.lastNextAttempt = next
	if s.messages[s.claimed-1].Attempts >= maxAttempts {
		s.poisoned++
	} else {
		s.retried++
	}
	return nil
}

type fakePublisher struct {
	err   error
	calls int
}

func (p *fakePublisher) Publish(_ context.Context, _, _ string, _ queue.Envelope) error {
	p.calls++
	return p.err
}

func testMessage(t *testing.T, attempts int) Message {
	t.Helper()
	envelope, err := queue.NewEnvelope(queue.MessageJobQueued, "trace-1", "correlation-1", "causation-1", map[string]any{
		"jobId": "77777777-7777-4777-8777-777777777777",
	})
	if err != nil {
		t.Fatalf("NewEnvelope returned error: %v", err)
	}
	return Message{ID: envelope.MessageID, Envelope: envelope, Exchange: queue.CommandsExchange, RoutingKey: envelope.MessageType, Attempts: attempts}
}

func TestDispatcherPublishesAndAcknowledges(t *testing.T) {
	store := &fakeStore{messages: []Message{testMessage(t, 0)}}
	publisher := &fakePublisher{}
	dispatcher, err := NewDispatcher(store, publisher, DispatcherConfig{LeaseDuration: time.Minute, RetryBase: time.Second, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("NewDispatcher returned error: %v", err)
	}
	report, err := dispatcher.Dispatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if report.Claimed != 1 || report.Published != 1 || store.published != 1 || publisher.calls != 1 {
		t.Fatalf("unexpected successful dispatch: report=%+v store=%+v publisher=%+v", report, store, publisher)
	}
}

func TestDispatcherSchedulesRetryAndPoisonsAfterBound(t *testing.T) {
	store := &fakeStore{messages: []Message{testMessage(t, 1), testMessage(t, 2)}}
	publisher := &fakePublisher{err: errors.New("broker unavailable")}
	dispatcher, err := NewDispatcher(store, publisher, DispatcherConfig{LeaseDuration: time.Minute, RetryBase: time.Second, MaxAttempts: 2})
	if err != nil {
		t.Fatalf("NewDispatcher returned error: %v", err)
	}
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	dispatcher.Now = func() time.Time { return base }
	first, err := dispatcher.Dispatch(context.Background(), 10)
	if err != nil || first.Retried != 1 || first.Poisoned != 0 || store.retried != 1 {
		t.Fatalf("unexpected first retry result: report=%+v err=%v store=%+v", first, err, store)
	}
	if !store.lastNextAttempt.Equal(base.Add(time.Second)) {
		t.Fatalf("unexpected exponential retry time: %v", store.lastNextAttempt)
	}
	second, err := dispatcher.Dispatch(context.Background(), 10)
	if err != nil || second.Poisoned != 1 || store.poisoned != 1 {
		t.Fatalf("unexpected poison result: report=%+v err=%v store=%+v", second, err, store)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := retryDelay(time.Second, 1); got != time.Second {
		t.Fatalf("unexpected first retry delay: %v", got)
	}
	if got := retryDelay(time.Hour, 10); got != MaxRetryDelay {
		t.Fatalf("retry delay exceeded bound: %v", got)
	}
}

package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
)

type Publisher interface {
	Publish(context.Context, string, string, queue.Envelope) error
}

type DispatcherConfig struct {
	LeaseDuration time.Duration
	RetryBase     time.Duration
	MaxAttempts   int
}

type Dispatcher struct {
	Store     Store
	Publisher Publisher
	Config    DispatcherConfig
	Now       func() time.Time
}

type DispatchReport struct {
	Claimed   int
	Published int
	Retried   int
	Poisoned  int
	Failed    int
	Failures  []error
}

func NewDispatcher(store Store, publisher Publisher, config DispatcherConfig) (*Dispatcher, error) {
	if store == nil || publisher == nil {
		return nil, errors.New("outbox dispatcher dependencies are required")
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = DefaultLease
	}
	if config.RetryBase == 0 {
		config.RetryBase = DefaultRetry
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 5
	}
	if config.LeaseDuration <= 0 || config.RetryBase <= 0 || config.MaxAttempts < 1 {
		return nil, errors.New("outbox dispatcher timing and attempt limits are invalid")
	}
	return &Dispatcher{Store: store, Publisher: publisher, Config: config, Now: time.Now}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, limit int) (DispatchReport, error) {
	if d == nil || d.Store == nil || d.Publisher == nil {
		return DispatchReport{}, errors.New("outbox dispatcher is not configured")
	}
	if limit < 1 || limit > 1000 {
		return DispatchReport{}, errors.New("outbox dispatch limit must be between 1 and 1000")
	}
	now := d.now()
	messages, err := d.Store.Claim(ctx, now, d.Config.LeaseDuration, limit)
	if err != nil {
		return DispatchReport{}, err
	}
	report := DispatchReport{Claimed: len(messages), Failures: make([]error, 0)}
	for _, message := range messages {
		if err := d.Publisher.Publish(ctx, message.Exchange, message.RoutingKey, message.Envelope); err != nil {
			report.Failed++
			nextAttempt := now.Add(retryDelay(d.Config.RetryBase, message.Attempts))
			if retryErr := d.Store.MarkRetry(ctx, message.ID, message.LeaseToken, nextAttempt, err.Error(), d.Config.MaxAttempts); retryErr != nil {
				report.Failures = append(report.Failures, fmt.Errorf("message %s publish failed: %v; retry state failed: %w", message.ID, err, retryErr))
				continue
			}
			if message.Attempts >= d.Config.MaxAttempts {
				report.Poisoned++
			} else {
				report.Retried++
			}
			report.Failures = append(report.Failures, fmt.Errorf("message %s publish failed: %w", message.ID, err))
			continue
		}
		if err := d.Store.MarkPublished(ctx, message.ID, message.LeaseToken, now); err != nil {
			report.Failed++
			report.Failures = append(report.Failures, fmt.Errorf("message %s publish acknowledgement failed: %w", message.ID, err))
			continue
		}
		report.Published++
	}
	return report, nil
}

func retryDelay(base time.Duration, attempts int) time.Duration {
	if attempts <= 1 {
		return base
	}
	delay := base
	for index := 1; index < attempts && delay < MaxRetryDelay; index++ {
		if delay > MaxRetryDelay/2 {
			return MaxRetryDelay
		}
		delay *= 2
	}
	if delay > MaxRetryDelay {
		return MaxRetryDelay
	}
	return delay
}

func (d *Dispatcher) now() time.Time {
	if d.Now == nil {
		return time.Now().UTC()
	}
	return d.Now().UTC()
}

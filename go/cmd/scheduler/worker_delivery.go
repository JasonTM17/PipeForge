package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/workerregistry"
	"github.com/rabbitmq/amqp091-go"
)

var workerEventRetryDelays = []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second}

func (r *schedulerRuntime) handleWorkerDelivery(ctx context.Context, delivery amqp091.Delivery) {
	if err := r.processWorkerDelivery(ctx, delivery); err != nil {
		r.logger.ErrorContext(ctx, "worker event handling failed", "message_id", delivery.MessageId, "message_type", delivery.Type, "error", err)
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			r.logger.ErrorContext(ctx, "worker event dead-letter failed", "message_id", delivery.MessageId, "error", nackErr)
		}
		return
	}
	if err := delivery.Ack(false); err != nil {
		r.logger.ErrorContext(ctx, "worker event acknowledgement failed", "message_id", delivery.MessageId, "error", err)
	}
}

func (r *schedulerRuntime) processWorkerDelivery(ctx context.Context, delivery amqp091.Delivery) error {
	envelope, err := queue.UnmarshalEnvelope(delivery.Body)
	if err != nil {
		return fmt.Errorf("decode worker event: %w", err)
	}
	if envelope.MessageType != queue.MessageWorkerRegistered && envelope.MessageType != queue.MessageWorkerHeartbeat {
		return fmt.Errorf("unexpected worker event type %q", envelope.MessageType)
	}
	if err := applyWorkerEnvelopeWithRetry(ctx, r.workerRegistry, envelope, workerEventRetryDelays); err != nil {
		return fmt.Errorf("persist worker event: %w", err)
	}
	return nil
}

func applyWorkerEnvelopeWithRetry(ctx context.Context, store workerEventStore, envelope queue.Envelope, delays []time.Duration) error {
	if store == nil {
		return errors.New("worker event store is not configured")
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = store.ApplyEnvelope(ctx, envelope)
		if err == nil || errors.Is(err, workerregistry.ErrInvalidEvent) || attempt >= len(delays) {
			return err
		}
		delay := delays[attempt]
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

package queue

import (
	"context"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

func waitForConfirmation(ctx context.Context, confirmations <-chan amqp091.Confirmation, timeout time.Duration) (amqp091.Confirmation, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case confirmation, ok := <-confirmations:
		return confirmation, ok
	case <-ctx.Done():
		return amqp091.Confirmation{}, false
	case <-timer.C:
		return amqp091.Confirmation{}, false
	}
}

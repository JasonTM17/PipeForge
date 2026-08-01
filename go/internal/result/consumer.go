package result

import (
	"context"
	"errors"
	"fmt"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
)

type DeliveryAcknowledger interface {
	Ack(bool) error
	Nack(bool, bool) error
	Reject(bool) error
}

func HandleDelivery(ctx context.Context, processor Processor, delivery DeliveryAcknowledger, body []byte) error {
	if processor == nil || delivery == nil {
		return errors.New("result delivery dependencies are required")
	}
	envelope, err := queue.UnmarshalEnvelope(body)
	if err != nil {
		if rejectErr := delivery.Reject(false); rejectErr != nil {
			return errors.Join(fmt.Errorf("reject malformed result event: %w", err), rejectErr)
		}
		return fmt.Errorf("reject malformed result event: %w", err)
	}
	if _, err := processor.Process(ctx, envelope); err != nil {
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			return errors.Join(fmt.Errorf("dead-letter result event: %w", err), nackErr)
		}
		return fmt.Errorf("dead-letter result event: %w", err)
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("ack result event: %w", err)
	}
	return nil
}

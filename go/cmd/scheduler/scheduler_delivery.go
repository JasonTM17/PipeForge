package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

type queuedJobSignal struct {
	JobID uuid.UUID `json:"jobId"`
}

func (r *schedulerRuntime) handleDelivery(ctx context.Context, delivery amqp091.Delivery) {
	if err := r.processDelivery(ctx, delivery); err != nil {
		r.logger.ErrorContext(ctx, "scheduler delivery handling failed", "message_id", delivery.MessageId, "message_type", delivery.Type, "error", err)
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			r.logger.ErrorContext(ctx, "scheduler delivery dead-letter failed", "message_id", delivery.MessageId, "error", nackErr)
		}
		return
	}
	if err := delivery.Ack(false); err != nil {
		r.logger.ErrorContext(ctx, "scheduler delivery acknowledgement failed", "message_id", delivery.MessageId, "error", err)
	}
}

func (r *schedulerRuntime) processDelivery(ctx context.Context, delivery amqp091.Delivery) error {
	envelope, err := queue.UnmarshalEnvelope(delivery.Body)
	if err != nil {
		return fmt.Errorf("decode dispatch signal: %w", err)
	}
	if envelope.MessageType != queuedJobMessageType {
		return fmt.Errorf("unexpected scheduler message type %q", envelope.MessageType)
	}
	signal, err := decodeQueuedJobSignal(envelope.Payload)
	if err != nil {
		return fmt.Errorf("validate dispatch signal: %w", err)
	}
	if err := r.dispatchEligible(ctx); err != nil {
		return err
	}
	r.logger.DebugContext(ctx, "scheduler signal reconciled", "job_id", signal.JobID)
	return r.dispatchOutbox(ctx)
}

func decodeQueuedJobSignal(payload []byte) (queuedJobSignal, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var signal queuedJobSignal
	if err := decoder.Decode(&signal); err != nil {
		return queuedJobSignal{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return queuedJobSignal{}, fmt.Errorf("payload must contain exactly one JSON object")
	}
	if signal.JobID == uuid.Nil {
		return queuedJobSignal{}, fmt.Errorf("jobId must be a non-zero UUID")
	}
	return signal, nil
}

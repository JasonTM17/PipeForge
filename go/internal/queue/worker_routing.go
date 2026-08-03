package queue

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

func WorkerRoutingKey(messageType string, workerID uuid.UUID) (string, error) {
	if workerID == uuid.Nil {
		return "", errors.New("worker routing requires a non-zero worker ID")
	}
	if messageType != MessageJobRequested && messageType != MessageJobCancel {
		return "", fmt.Errorf("message type %q cannot use worker routing", messageType)
	}
	return messageType + "." + workerID.String(), nil
}

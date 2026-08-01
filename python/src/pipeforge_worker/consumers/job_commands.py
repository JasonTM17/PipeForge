"""Validated job command intake with explicit durable rejection semantics."""

from __future__ import annotations

import logging
import time
from collections.abc import Awaitable, Callable

from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.contracts.validator import ContractValidationError, ContractValidator
from pipeforge_worker.messaging.protocols import (
    Delivery,
    MessageDisposition,
    PermanentMessageError,
    TransientMessageError,
)
from pipeforge_worker.observability.metrics import WorkerMetrics

JobCommandHandler = Callable[[Envelope], Awaitable[None]]


class RejectingJobHandler:
    """Phase 9 classifier used until the processing pipeline is installed."""

    async def __call__(self, _envelope: Envelope) -> None:
        raise PermanentMessageError("processing pipeline is not enabled")


class JobCommandConsumer:
    def __init__(
        self,
        validator: ContractValidator,
        handler: JobCommandHandler,
        metrics: WorkerMetrics,
    ) -> None:
        self._validator = validator
        self._handler = handler
        self._metrics = metrics
        self._logger = logging.getLogger(__name__)

    async def handle(self, delivery: Delivery) -> MessageDisposition:
        message_type = delivery.message_type[:64] or "unknown"
        self._metrics.messages_received.labels(message_type=message_type).inc()
        started_at = time.perf_counter()
        try:
            value = self._validator.validate_bytes(delivery.body)
            envelope = Envelope.from_mapping(value)
            if envelope.message_type != "processing.job.requested":
                raise PermanentMessageError("message is not a processing job request")
            await self._handler(envelope)
        except ContractValidationError:
            self._metrics.messages_rejected.labels(reason="invalid_contract").inc()
            return MessageDisposition.REJECT
        except PermanentMessageError:
            self._metrics.messages_rejected.labels(reason="permanent").inc()
            return MessageDisposition.REJECT
        except TransientMessageError:
            self._metrics.messages_rejected.labels(reason="transient").inc()
            return MessageDisposition.REJECT
        except Exception:
            self._logger.exception("unexpected job command failure; classifying to DLQ")
            self._metrics.messages_rejected.labels(reason="internal").inc()
            return MessageDisposition.REJECT
        finally:
            self._metrics.handler_duration_seconds.observe(time.perf_counter() - started_at)
        self._metrics.messages_acked.labels(message_type=message_type).inc()
        return MessageDisposition.ACK

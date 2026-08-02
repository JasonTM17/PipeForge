"""Validated job command intake with explicit durable rejection semantics."""

from __future__ import annotations

import logging
import time
from collections.abc import Awaitable, Callable
from uuid import UUID

from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.contracts.validator import ContractValidationError, ContractValidator
from pipeforge_worker.messaging.protocols import (
    Delivery,
    MessageDisposition,
    PermanentMessageError,
    TransientMessageError,
)
from pipeforge_worker.observability.metrics import WorkerMetrics
from pipeforge_worker.worker.cancellation import CancellationRegistry

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
        cancellation_registry: CancellationRegistry | None = None,
    ) -> None:
        self._validator = validator
        self._handler = handler
        self._cancellation_registry = cancellation_registry
        self._metrics = metrics
        self._logger = logging.getLogger(__name__)

    async def handle(self, delivery: Delivery) -> MessageDisposition:
        message_type = delivery.message_type[:64] or "unknown"
        self._metrics.messages_received.labels(message_type=message_type).inc()
        started_at = time.perf_counter()
        try:
            value = self._validator.validate_bytes(delivery.body)
            envelope = Envelope.from_mapping(value)
            if envelope.message_type == "processing.job.cancel-requested":
                self._record_cancellation(envelope)
            elif envelope.message_type == "processing.job.requested":
                await self._handler(envelope)
            else:
                raise PermanentMessageError("message is not a processing job request")
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

    def _record_cancellation(self, envelope: Envelope) -> None:
        if self._cancellation_registry is None:
            raise PermanentMessageError("cancellation registry is not configured")
        payload = envelope.payload
        try:
            job_id = UUID(str(payload["jobId"]))
            attempt_id = _optional_uuid(payload.get("attemptId"), "attemptId")
            lease_id = _optional_uuid(payload.get("leaseId"), "leaseId")
            if attempt_id is None or lease_id is None:
                raise PermanentMessageError(
                    "worker cancellation command requires an attempt and lease fence"
                )
            reason = str(payload["reason"])
            self._cancellation_registry.request(job_id, reason, attempt_id, lease_id)
        except PermanentMessageError:
            raise
        except (KeyError, TypeError, ValueError) as exc:
            raise PermanentMessageError("cancellation command identity is invalid") from exc


def _optional_uuid(value: object, name: str) -> UUID | None:
    if value is None:
        return None
    try:
        parsed = UUID(str(value))
    except (AttributeError, ValueError) as exc:
        raise ValueError(f"{name} is not a UUID") from exc
    if parsed.int == 0:
        raise ValueError(f"{name} must be non-zero")
    return parsed

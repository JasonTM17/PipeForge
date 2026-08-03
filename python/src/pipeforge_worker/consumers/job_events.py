"""Schema-validated result event publication for one worker execution."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from pipeforge_worker.consumers.job_execution_support import JobIdentity
from pipeforge_worker.contracts.envelope import Envelope, build_envelope
from pipeforge_worker.contracts.validator import ContractValidationError, ContractValidator
from pipeforge_worker.messaging.protocols import (
    PermanentMessageError,
    Publisher,
    TransientMessageError,
)


class JobEventPublisher:
    """Build, validate, and publish result events before a command is acknowledged."""

    def __init__(self, publisher: Publisher, validator: ContractValidator) -> None:
        self._publisher = publisher
        self._validator = validator

    async def publish(
        self,
        command: Envelope,
        message_type: str,
        payload: Mapping[str, Any],
    ) -> None:
        event = build_envelope(
            message_type,
            command.trace_id,
            command.correlation_id,
            str(command.message_id),
            payload,
        )
        try:
            self._validator.validate_bytes(event.to_bytes())
        except ContractValidationError as exc:
            raise PermanentMessageError(
                f"generated {message_type} event failed validation"
            ) from exc
        try:
            await self._publisher.publish(event, message_type)
        except Exception as exc:
            raise TransientMessageError(f"publish {message_type} event failed") from exc

    async def publish_failure(
        self,
        command: Envelope,
        identity: JobIdentity,
        code: str,
        message: str,
        retryable: bool,
    ) -> None:
        await self.publish(
            command,
            "processing.job.failed",
            {
                "jobId": str(identity.job_id),
                "attemptId": str(identity.attempt_id),
                "leaseId": str(identity.lease_id),
                "workerId": str(identity.worker_id),
                "error": {"code": code, "message": message[:500], "retryable": retryable},
            },
        )

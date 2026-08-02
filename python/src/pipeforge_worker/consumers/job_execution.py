"""Cancellation-aware job execution and versioned result publication."""

from __future__ import annotations

import inspect
from collections.abc import Mapping
from typing import Any
from uuid import UUID

from pipeforge_worker.consumers.job_execution_support import (
    CleanupHandler,
    JobIdentity,
    ProcessingRunner,
    ProcessingRunResult,
    parse_identity,
    progress_payload,
)
from pipeforge_worker.contracts.envelope import Envelope, build_envelope
from pipeforge_worker.contracts.validator import ContractValidationError, ContractValidator
from pipeforge_worker.messaging.protocols import (
    PermanentMessageError,
    Publisher,
    TransientMessageError,
)
from pipeforge_worker.processors.protocols import (
    CancellationRequested,
    ProcessingContext,
    ProgressSnapshot,
)
from pipeforge_worker.worker.cancellation import CancellationRegistry
from pipeforge_worker.worker.lifecycle import WorkerLifecycle


class ProcessingJobExecutor:
    """Run one leased job and publish a durable terminal event before acking it."""

    def __init__(
        self,
        publisher: Publisher,
        validator: ContractValidator,
        cancellations: CancellationRegistry,
        worker_id: UUID,
        runner: ProcessingRunner,
        lifecycle: WorkerLifecycle | None = None,
        cleanup: CleanupHandler | None = None,
    ) -> None:
        if worker_id.int == 0:
            raise ValueError("worker_id is required")
        self._publisher = publisher
        self._validator = validator
        self._cancellations = cancellations
        self._worker_id = worker_id
        self._runner = runner
        self._lifecycle = lifecycle
        self._cleanup = cleanup

    async def __call__(self, envelope: Envelope) -> None:
        identity = parse_identity(envelope, self._worker_id)
        if self._lifecycle is not None:
            self._lifecycle.add_job(str(identity.job_id))
        progress: list[ProgressSnapshot] = []
        context = ProcessingContext(
            job_id=identity.job_id,
            attempt_id=identity.attempt_id,
            lease_id=identity.lease_id,
            worker_id=identity.worker_id,
            is_cancelled=lambda: self._cancellations.is_cancelled(
                identity.job_id, identity.attempt_id, identity.lease_id
            ),
            on_progress=progress.append,
        )
        try:
            await self._publish(
                envelope,
                "processing.job.started",
                {
                    "jobId": str(identity.job_id),
                    "attemptId": str(identity.attempt_id),
                    "leaseId": str(identity.lease_id),
                    "workerId": str(identity.worker_id),
                    "attemptNumber": identity.attempt_number,
                },
            )
            context.check_cancellation()
            run_result = self._runner(envelope, context)
            if inspect.isawaitable(run_result):
                run_result = await run_result
            context.check_cancellation()
            if not isinstance(run_result, ProcessingRunResult):
                raise TypeError("processing runner returned an invalid result")
            for snapshot in progress:
                await self._publish(
                    envelope, "processing.job.progressed", progress_payload(identity, snapshot)
                )
            await self._publish(
                envelope,
                "processing.job.succeeded",
                {
                    "jobId": str(identity.job_id),
                    "attemptId": str(identity.attempt_id),
                    "leaseId": str(identity.lease_id),
                    "workerId": str(identity.worker_id),
                    "artifacts": list(run_result.artifacts),
                },
            )
        except CancellationRequested:
            reason = (
                self._cancellations.reason_for(
                    identity.job_id, identity.attempt_id, identity.lease_id
                )
                or "cancellation_requested"
            )
            await self._run_cleanup(identity)
            await self._publish(
                envelope,
                "processing.job.cancelled",
                {
                    "jobId": str(identity.job_id),
                    "attemptId": str(identity.attempt_id),
                    "leaseId": str(identity.lease_id),
                    "workerId": str(identity.worker_id),
                    "reason": reason,
                },
            )
        except TransientMessageError:
            raise
        except PermanentMessageError as exc:
            await self._publish_failure(envelope, identity, "INPUT_INVALID", str(exc), False)
        except Exception:
            await self._publish_failure(
                envelope, identity, "WORKER_FAILURE", "worker processing failed", True
            )
        finally:
            self._cancellations.clear_if_matches(
                identity.job_id, identity.attempt_id, identity.lease_id
            )
            if self._lifecycle is not None:
                self._lifecycle.remove_job(str(identity.job_id))

    async def _publish_failure(
        self,
        command: Envelope,
        identity: JobIdentity,
        code: str,
        message: str,
        retryable: bool,
    ) -> None:
        await self._publish(
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

    async def _run_cleanup(self, identity: JobIdentity) -> None:
        if self._cleanup is None:
            return
        try:
            result = self._cleanup(identity)
            if inspect.isawaitable(result):
                await result
        except Exception as exc:
            raise TransientMessageError("cancellation cleanup failed") from exc

    async def _publish(
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

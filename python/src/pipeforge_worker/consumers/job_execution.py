"""Cancellation-aware job execution and versioned result publication."""

from __future__ import annotations

import asyncio
import inspect
from uuid import UUID

from pipeforge_worker.consumers.job_events import JobEventPublisher
from pipeforge_worker.consumers.job_execution_support import (
    CleanupHandler,
    JobIdentity,
    ProcessingRunner,
    ProcessingRunResult,
    parse_identity,
    progress_payload,
)
from pipeforge_worker.consumers.job_progress import ProgressRelay
from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import (
    PermanentMessageError,
    Publisher,
    TransientMessageError,
)
from pipeforge_worker.processors.protocols import (
    CancellationRequested,
    ProcessingContext,
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
        self._events = JobEventPublisher(publisher, validator)
        self._cancellations = cancellations
        self._worker_id = worker_id
        self._runner = runner
        self._lifecycle = lifecycle
        self._cleanup = cleanup

    async def __call__(self, envelope: Envelope) -> None:
        identity = parse_identity(envelope, self._worker_id)
        if self._lifecycle is not None:
            self._lifecycle.add_job(str(identity.job_id))
        progress = ProgressRelay()
        context = ProcessingContext(
            job_id=identity.job_id,
            attempt_id=identity.attempt_id,
            lease_id=identity.lease_id,
            worker_id=identity.worker_id,
            attempt_number=identity.attempt_number,
            is_cancelled=lambda: self._cancellations.is_cancelled(
                identity.job_id, identity.attempt_id, identity.lease_id
            ),
            on_progress=progress.emit,
        )
        try:
            await self._events.publish(
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
            run_result = await self._run_with_streamed_progress(
                envelope, context, identity, progress
            )
            context.check_cancellation()
            if not isinstance(run_result, ProcessingRunResult):
                raise TypeError("processing runner returned an invalid result")
            for artifact in run_result.artifacts:
                context.check_cancellation()
                await self._events.publish(
                    envelope,
                    "processing.artifact.created",
                    {
                        "jobId": str(identity.job_id),
                        "attemptId": str(identity.attempt_id),
                        "leaseId": str(identity.lease_id),
                        "artifactId": str(artifact.artifact_id),
                        "kind": artifact.kind,
                        "objectKey": artifact.object_key,
                        "sizeBytes": artifact.size_bytes,
                        "contentType": artifact.content_type,
                        "checksumSha256": artifact.checksum_sha256,
                    },
                )
            await self._events.publish(
                envelope,
                "processing.job.succeeded",
                {
                    "jobId": str(identity.job_id),
                    "attemptId": str(identity.attempt_id),
                    "leaseId": str(identity.lease_id),
                    "workerId": str(identity.worker_id),
                    "artifacts": [artifact.object_key for artifact in run_result.artifacts],
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
            await self._events.publish(
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
            await self._events.publish_failure(envelope, identity, "INPUT_INVALID", str(exc), False)
        except Exception:
            await self._events.publish_failure(
                envelope, identity, "WORKER_FAILURE", "worker processing failed", True
            )
        finally:
            self._cancellations.clear_if_matches(
                identity.job_id, identity.attempt_id, identity.lease_id
            )
            if self._lifecycle is not None:
                self._lifecycle.remove_job(str(identity.job_id))

    async def _run_with_streamed_progress(
        self,
        envelope: Envelope,
        context: ProcessingContext,
        identity: JobIdentity,
        progress: ProgressRelay,
    ) -> object:
        async def invoke() -> object:
            result = self._runner(envelope, context)
            if inspect.isawaitable(result):
                return await result
            return result

        runner = asyncio.create_task(invoke(), name=f"job-{identity.job_id}")
        return await progress.wait_for(
            runner,
            lambda snapshot: self._events.publish(
                envelope, "processing.job.progressed", progress_payload(identity, snapshot)
            ),
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

"""Typed identity and payload helpers for worker result events."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, Mapping
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from uuid import UUID

from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.messaging.protocols import PermanentMessageError
from pipeforge_worker.processors.protocols import ProcessingContext, ProgressSnapshot


@dataclass(frozen=True, slots=True)
class JobIdentity:
    job_id: UUID
    attempt_id: UUID
    lease_id: UUID
    worker_id: UUID
    attempt_number: int


@dataclass(frozen=True, slots=True)
class ProcessingRunResult:
    artifacts: tuple[str, ...] = ()


ProcessingRunner = Callable[
    [Envelope, ProcessingContext], Awaitable[ProcessingRunResult] | ProcessingRunResult
]
CleanupHandler = Callable[[JobIdentity], Awaitable[None] | None]


def parse_identity(envelope: Envelope, worker_id: UUID) -> JobIdentity:
    payload = envelope.payload
    try:
        job_id = required_uuid(payload, "jobId")
        attempt_id = required_uuid(payload, "attemptId")
        lease_id = required_uuid(payload, "leaseId")
        command_worker_id = optional_uuid(payload.get("workerId")) or worker_id
        attempt_number = int(payload.get("attemptNumber", 1))
    except (KeyError, TypeError, ValueError) as exc:
        raise PermanentMessageError("job command is missing a valid lease identity") from exc
    if command_worker_id != worker_id or attempt_number < 1:
        raise PermanentMessageError("job command lease identity is not owned by this worker")
    return JobIdentity(job_id, attempt_id, lease_id, command_worker_id, attempt_number)


def required_uuid(payload: Mapping[str, Any], key: str) -> UUID:
    value = payload[key]
    parsed = UUID(str(value))
    if parsed.int == 0:
        raise ValueError(f"{key} must be non-zero")
    return parsed


def optional_uuid(value: object) -> UUID | None:
    if value is None:
        return None
    parsed = UUID(str(value))
    if parsed.int == 0:
        raise ValueError("UUID must be non-zero")
    return parsed


def progress_payload(identity: JobIdentity, snapshot: ProgressSnapshot) -> dict[str, Any]:
    percent = 0.0 if snapshot.fraction is None else min(100.0, snapshot.fraction * 100)
    return {
        "jobId": str(identity.job_id),
        "attemptId": str(identity.attempt_id),
        "leaseId": str(identity.lease_id),
        "stage": snapshot.stage,
        "processedRows": snapshot.rows_processed,
        "estimatedTotalRows": snapshot.estimated_rows,
        "progressPercent": percent,
        "throughput": snapshot.throughput,
        "updatedAt": format_datetime(snapshot.updated_at),
    }


def format_datetime(value: datetime) -> str:
    return value.astimezone(UTC).isoformat().replace("+00:00", "Z")

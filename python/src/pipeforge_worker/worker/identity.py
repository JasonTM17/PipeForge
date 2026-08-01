"""Worker registration and throttled heartbeat message generation."""

from __future__ import annotations

import asyncio
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from enum import StrEnum
from uuid import UUID

from pipeforge_worker.config import Settings
from pipeforge_worker.contracts.envelope import Envelope, build_envelope
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import Publisher
from pipeforge_worker.observability.metrics import WorkerMetrics


class WorkerStatus(StrEnum):
    STARTING = "STARTING"
    READY = "READY"
    BUSY = "BUSY"
    DRAINING = "DRAINING"
    UNHEALTHY = "UNHEALTHY"
    OFFLINE = "OFFLINE"


@dataclass(frozen=True, slots=True)
class WorkerIdentity:
    worker_id: UUID
    instance_id: str
    hostname: str
    supported_operations: tuple[str, ...]
    software_version: str
    max_concurrency: int

    @classmethod
    def from_settings(cls, settings: Settings) -> WorkerIdentity:
        return cls(
            worker_id=settings.worker_id,
            instance_id=settings.instance_id,
            hostname=settings.hostname,
            supported_operations=settings.supported_operations,
            software_version=settings.software_version,
            max_concurrency=settings.max_concurrency,
        )

    def registration_envelope(self, now: datetime) -> Envelope:
        trace_id = f"worker:{self.worker_id}"
        return build_envelope(
            "processing.worker.registered",
            trace_id,
            str(self.worker_id),
            str(self.worker_id),
            {
                "workerId": str(self.worker_id),
                "instanceId": self.instance_id,
                "hostname": self.hostname,
                "supportedOperations": list(self.supported_operations[:64]),
                "softwareVersion": self.software_version,
                "maxConcurrency": self.max_concurrency,
                "startedAt": _utc_iso(now),
            },
            clock=lambda: now,
        )

    def heartbeat_envelope(
        self,
        status: WorkerStatus,
        current_job_ids: list[str] | tuple[str, ...],
        now: datetime,
    ) -> Envelope:
        trace_id = f"worker:{self.worker_id}"
        bounded_job_ids = sorted(set(current_job_ids))[:128]
        return build_envelope(
            "processing.worker.heartbeat",
            trace_id,
            str(self.worker_id),
            str(self.worker_id),
            {
                "workerId": str(self.worker_id),
                "instanceId": self.instance_id,
                "hostname": self.hostname,
                "supportedOperations": list(self.supported_operations[:64]),
                "softwareVersion": self.software_version,
                "status": status.value,
                "currentConcurrency": len(bounded_job_ids),
                "maxConcurrency": self.max_concurrency,
                "currentJobIds": bounded_job_ids,
                "observedAt": _utc_iso(now),
            },
            clock=lambda: now,
        )


class HeartbeatController:
    def __init__(
        self,
        identity: WorkerIdentity,
        publisher: Publisher,
        validator: ContractValidator,
        metrics: WorkerMetrics,
        events_exchange: str,
        interval_seconds: float,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        self.identity = identity
        self.publisher = publisher
        self.validator = validator
        self.metrics = metrics
        self.events_exchange = events_exchange
        self.interval_seconds = interval_seconds
        self.clock = clock or (lambda: datetime.now(UTC))
        self._last_heartbeat: datetime | None = None

    async def publish_registration(self) -> None:
        envelope = self.identity.registration_envelope(self.clock())
        self.validator.validate_mapping(envelope.to_mapping())
        await self.publisher.publish(envelope, envelope.message_type)
        self.metrics.heartbeat_publications.labels(message_type=envelope.message_type).inc()

    async def publish_heartbeat(
        self,
        status: WorkerStatus,
        current_job_ids: list[str] | tuple[str, ...],
        force: bool = False,
    ) -> bool:
        now = self.clock()
        if not force and self._last_heartbeat is not None:
            if (now - self._last_heartbeat).total_seconds() < self.interval_seconds:
                return False
        envelope = self.identity.heartbeat_envelope(status, current_job_ids, now)
        self.validator.validate_mapping(envelope.to_mapping())
        await self.publisher.publish(envelope, envelope.message_type)
        self._last_heartbeat = now
        self.metrics.heartbeat_publications.labels(message_type=envelope.message_type).inc()
        return True

    async def run(
        self,
        stop_event: asyncio.Event,
        snapshot: Callable[[], tuple[WorkerStatus, tuple[str, ...]]],
    ) -> None:
        while not stop_event.is_set():
            status, current_job_ids = snapshot()
            await self.publish_heartbeat(status, current_job_ids)
            try:
                await asyncio.wait_for(stop_event.wait(), timeout=self.interval_seconds)
            except TimeoutError:
                continue


def _utc_iso(value: datetime) -> str:
    return value.astimezone(UTC).isoformat().replace("+00:00", "Z")

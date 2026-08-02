"""Application assembly; all external clients are constructed here."""

from __future__ import annotations

from collections.abc import Iterator, Mapping
from contextlib import contextmanager
from typing import BinaryIO

from pipeforge_worker.config import Settings
from pipeforge_worker.consumers.job_commands import JobCommandConsumer, RejectingJobHandler
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import (
    Consumer,
    Delivery,
    MessageDisposition,
    MessageHandler,
    Publisher,
)
from pipeforge_worker.messaging.rabbitmq import CompositeConsumer, RabbitConsumer, RabbitPublisher
from pipeforge_worker.observability.health import HealthServer, HealthState
from pipeforge_worker.observability.metrics import WorkerMetrics
from pipeforge_worker.runtime import PingingObjectStore, WorkerRuntime
from pipeforge_worker.storage.minio import MinioObjectStore, StoredObject
from pipeforge_worker.worker.cancellation import CancellationRegistry
from pipeforge_worker.worker.identity import HeartbeatController, WorkerIdentity
from pipeforge_worker.worker.lifecycle import WorkerLifecycle


class IdlePublisher(Publisher):
    async def publish(
        self,
        _envelope: object,
        _routing_key: str,
        _headers: Mapping[str, object] | None = None,
    ) -> None:
        return

    async def close(self) -> None:
        return


class IdleConsumer(Consumer):
    async def start(self, _handler: MessageHandler) -> None:
        return

    async def stop(self) -> None:
        return


class IdleStorage(PingingObjectStore):
    def ping(self) -> None:
        return

    @contextmanager
    def download_stream(self, _key: str) -> Iterator[BinaryIO]:
        raise RuntimeError("health-only storage is not available")
        yield  # pragma: no cover

    def upload_stream(
        self, _key: str, _stream: BinaryIO, _size: int, _content_type: str
    ) -> StoredObject:
        raise RuntimeError("health-only storage is not available")

    def close(self) -> None:
        return


class IdleCommandHandler:
    async def __call__(self, _delivery: Delivery) -> MessageDisposition:
        return MessageDisposition.REJECT


def build_runtime(settings: Settings) -> WorkerRuntime:
    metrics = WorkerMetrics()
    health = HealthState(settings.max_concurrency)
    health_server = HealthServer(health, metrics, settings.health_host, settings.health_port)
    lifecycle = WorkerLifecycle()

    if settings.health_only:
        return WorkerRuntime(
            settings,
            IdlePublisher(),
            IdleConsumer(),
            IdleStorage(),
            IdleCommandHandler(),
            None,
            lifecycle,
            health,
            health_server,
            metrics,
        )

    validator = ContractValidator(settings.contracts_dir, settings.max_message_bytes)
    identity = WorkerIdentity.from_settings(settings)
    publisher = RabbitPublisher(
        settings.broker_url,
        settings.events_exchange,
        settings.reconnect_delay_seconds,
    )
    job_consumer = RabbitConsumer(
        settings.broker_url,
        settings.broker_queue,
        settings.prefetch_count,
        {
            "x-dead-letter-exchange": settings.dead_letter_exchange,
            "x-dead-letter-routing-key": settings.dead_letter_routing_key,
        },
        settings.reconnect_delay_seconds,
    )
    cancellation_consumer = RabbitConsumer(
        settings.broker_url,
        settings.cancellation_queue,
        settings.prefetch_count,
        {
            "x-dead-letter-exchange": settings.dead_letter_exchange,
            "x-dead-letter-routing-key": settings.cancellation_dead_letter_routing_key,
        },
        settings.reconnect_delay_seconds,
    )
    consumer = CompositeConsumer((job_consumer, cancellation_consumer))
    storage = MinioObjectStore(
        settings.minio_endpoint,
        settings.minio_access_key,
        settings.minio_secret_key,
        settings.minio_secure,
        settings.dataset_bucket,
    )
    heartbeat = HeartbeatController(
        identity,
        publisher,
        validator,
        metrics,
        settings.events_exchange,
        settings.heartbeat_interval_seconds,
    )
    command_consumer = JobCommandConsumer(
        validator,
        RejectingJobHandler(),
        metrics,
        cancellation_registry=CancellationRegistry(),
    )
    return WorkerRuntime(
        settings,
        publisher,
        consumer,
        storage,
        command_consumer.handle,
        heartbeat,
        lifecycle,
        health,
        health_server,
        metrics,
    )

"""Application assembly; all external clients are constructed here."""

from __future__ import annotations

from collections.abc import Mapping

from pipeforge_worker.config import Settings
from pipeforge_worker.consumers.job_commands import JobCommandConsumer
from pipeforge_worker.consumers.job_execution import ProcessingJobExecutor
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import (
    Consumer,
    Delivery,
    MessageDisposition,
    MessageHandler,
    Publisher,
)
from pipeforge_worker.messaging.rabbitmq import (
    CompositeConsumer,
    RabbitBinding,
    RabbitConsumer,
    RabbitPublisher,
)
from pipeforge_worker.observability.health import HealthServer, HealthState
from pipeforge_worker.observability.metrics import WorkerMetrics
from pipeforge_worker.processors.job_runner import ProcessingJobRunner
from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.production_operation_dispatcher import (
    ProductionOperationDispatcher,
)
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.runtime import PingingObjectStore, WorkerRuntime
from pipeforge_worker.storage.minio import MinioObjectStore
from pipeforge_worker.storage.worker_stores import WorkerObjectStores
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

    def close(self) -> None:
        return


class IdleCommandHandler:
    async def __call__(self, _delivery: Delivery) -> MessageDisposition:
        return MessageDisposition.REJECT


def _dead_letter_arguments(
    dead_letter_exchange: str,
    dead_letter_routing_key: str,
) -> dict[str, object]:
    return {
        "x-dead-letter-exchange": dead_letter_exchange,
        "x-dead-letter-routing-key": dead_letter_routing_key,
    }


def _build_rabbit_consumer(
    settings: Settings,
    queue_name: str,
    dead_letter_routing_key: str,
    routing_key: str,
) -> RabbitConsumer:
    return RabbitConsumer(
        settings.broker_url,
        queue_name,
        settings.prefetch_count,
        _dead_letter_arguments(
            settings.dead_letter_exchange,
            dead_letter_routing_key,
        ),
        settings.reconnect_delay_seconds,
        (RabbitBinding(settings.commands_exchange, routing_key),),
    )


def _build_runtime_consumer(settings: Settings) -> CompositeConsumer:
    job_consumer = _build_rabbit_consumer(
        settings,
        settings.job_queue_name,
        settings.dead_letter_routing_key,
        settings.job_routing_key,
    )
    cancellation_consumer = _build_rabbit_consumer(
        settings,
        settings.cancellation_queue_name,
        settings.cancellation_dead_letter_routing_key,
        settings.cancellation_routing_key,
    )
    consumers: tuple[Consumer, ...] = (job_consumer, cancellation_consumer)
    job_shutdown_group: tuple[Consumer, ...] = (job_consumer,)
    cancellation_shutdown_group: tuple[Consumer, ...] = (cancellation_consumer,)
    if settings.legacy_shared_queue_compatibility:
        legacy_job_consumer = _build_rabbit_consumer(
            settings,
            settings.broker_queue,
            settings.dead_letter_routing_key,
            "processing.job.requested",
        )
        legacy_cancellation_consumer = _build_rabbit_consumer(
            settings,
            settings.cancellation_queue,
            settings.cancellation_dead_letter_routing_key,
            "processing.job.cancel-requested",
        )
        consumers += (legacy_job_consumer, legacy_cancellation_consumer)
        job_shutdown_group += (legacy_job_consumer,)
        cancellation_shutdown_group += (legacy_cancellation_consumer,)
    return CompositeConsumer(
        consumers,
        (job_shutdown_group, cancellation_shutdown_group),
    )


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
    consumer = _build_runtime_consumer(settings)
    storage = WorkerObjectStores(
        source=MinioObjectStore(
            settings.minio_endpoint,
            settings.minio_access_key,
            settings.minio_secret_key,
            settings.minio_secure,
            settings.dataset_bucket,
        ),
        artifacts=MinioObjectStore(
            settings.minio_endpoint,
            settings.minio_access_key,
            settings.minio_secret_key,
            settings.minio_secure,
            settings.artifact_bucket,
        ),
    )
    heartbeat = HeartbeatController(
        identity,
        publisher,
        validator,
        metrics,
        settings.events_exchange,
        settings.heartbeat_interval_seconds,
    )
    cancellations = CancellationRegistry()
    pipeline = ProcessingPipeline(DatasetReaderFactory(), ProductionOperationDispatcher())
    executor = ProcessingJobExecutor(
        publisher,
        validator,
        cancellations,
        identity.worker_id,
        ProcessingJobRunner(storage.source, storage.artifacts, pipeline),
        lifecycle,
    )
    command_consumer = JobCommandConsumer(
        validator,
        executor,
        metrics,
        cancellation_registry=cancellations,
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

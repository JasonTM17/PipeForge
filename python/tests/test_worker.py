import asyncio
import threading
from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import pytest

from pipeforge_worker.app import IdleCommandHandler, build_runtime
from pipeforge_worker.config import Settings
from pipeforge_worker.consumers.job_commands import JobCommandConsumer, RejectingJobHandler
from pipeforge_worker.consumers.job_execution import ProcessingJobExecutor, ProcessingRunResult
from pipeforge_worker.consumers.job_execution_support import ArtifactDescriptor
from pipeforge_worker.consumers.job_progress import ProgressRelay
from pipeforge_worker.contracts.envelope import build_envelope
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import Delivery, MessageDisposition, settle_delivery
from pipeforge_worker.messaging.rabbitmq import CompositeConsumer, RabbitConsumer
from pipeforge_worker.observability.health import HealthState
from pipeforge_worker.observability.metrics import WorkerMetrics
from pipeforge_worker.processors.protocols import ProgressSnapshot
from pipeforge_worker.runtime import WorkerRuntime
from pipeforge_worker.storage.worker_stores import WorkerObjectStores
from pipeforge_worker.worker.cancellation import CancellationRegistry
from pipeforge_worker.worker.identity import HeartbeatController, WorkerIdentity, WorkerStatus
from pipeforge_worker.worker.lifecycle import WorkerLifecycle


def _source() -> dict[str, object]:
    return {
        "objectKey": "datasets/fixture/versions/fixture/raw",
        "contentType": "text/csv",
        "format": "CSV",
        "sizeBytes": 64,
    }


class FakeDelivery(Delivery):
    def __init__(self, body: bytes, message_type: str = "processing.job.requested") -> None:
        self.body = body
        self.headers: dict[str, object] = {}
        self.message_type = message_type
        self.acked = False
        self.rejected = False

    async def ack(self) -> None:
        self.acked = True

    async def reject(self, requeue: bool = False) -> None:
        assert requeue is False
        self.rejected = True


class FakePublisher:
    def __init__(self) -> None:
        self.messages: list[tuple[object, str, object]] = []

    async def publish(self, envelope: object, routing_key: str, headers: object = None) -> None:
        self.messages.append((envelope, routing_key, headers))

    async def close(self) -> None:
        return


def _validator() -> ContractValidator:
    return ContractValidator(Path(__file__).resolve().parents[2] / "contracts" / "json-schema")


@pytest.mark.asyncio
async def test_invalid_job_is_rejected_without_crashing() -> None:
    consumer = JobCommandConsumer(_validator(), RejectingJobHandler(), WorkerMetrics())
    delivery = FakeDelivery(b'{"messageType":"invalid"}')
    disposition = await consumer.handle(delivery)
    await settle_delivery(delivery, lambda _delivery: _return_disposition(disposition))
    assert disposition is MessageDisposition.REJECT
    assert delivery.acked is False
    assert delivery.rejected is True


@pytest.mark.asyncio
async def test_rejecting_handler_durably_rejects_when_explicitly_selected() -> None:
    body = (
        Path(__file__).resolve().parents[2]
        / "contracts"
        / "examples"
        / "processing.job.requested.v1.json"
    ).read_bytes()
    consumer = JobCommandConsumer(_validator(), RejectingJobHandler(), WorkerMetrics())
    delivery = FakeDelivery(body)
    disposition = await consumer.handle(delivery)
    await settle_delivery(delivery, lambda _delivery: _return_disposition(disposition))
    assert disposition is MessageDisposition.REJECT
    assert delivery.rejected is True


@pytest.mark.asyncio
async def test_cancellation_command_is_fenced_to_attempt_and_lease() -> None:
    job_id, attempt_id, lease_id = uuid4(), uuid4(), uuid4()
    body = build_envelope(
        "processing.job.cancel-requested",
        "trace",
        str(job_id),
        "request",
        {
            "jobId": str(job_id),
            "attemptId": str(attempt_id),
            "leaseId": str(lease_id),
            "reason": "owner requested",
        },
    ).to_bytes()
    registry = CancellationRegistry()
    consumer = JobCommandConsumer(_validator(), RejectingJobHandler(), WorkerMetrics(), registry)
    delivery = FakeDelivery(body, "processing.job.cancel-requested")

    disposition = await consumer.handle(delivery)

    assert disposition is MessageDisposition.ACK
    assert registry.is_cancelled(job_id, attempt_id, lease_id)
    assert not registry.is_cancelled(job_id, uuid4(), lease_id)
    assert not registry.is_cancelled(job_id, attempt_id, uuid4())
    assert registry.reason_for(job_id, attempt_id, lease_id) == "owner requested"


@pytest.mark.asyncio
async def test_unfenced_cancellation_command_is_rejected() -> None:
    job_id = uuid4()
    body = build_envelope(
        "processing.job.cancel-requested",
        "trace",
        str(job_id),
        "request",
        {"jobId": str(job_id), "attemptId": None, "leaseId": None, "reason": "owner requested"},
    ).to_bytes()
    registry = CancellationRegistry()
    consumer = JobCommandConsumer(_validator(), RejectingJobHandler(), WorkerMetrics(), registry)

    disposition = await consumer.handle(FakeDelivery(body, "processing.job.cancel-requested"))

    assert disposition is MessageDisposition.REJECT
    assert registry.snapshot() == ()


@pytest.mark.asyncio
async def test_executor_publishes_cancelled_after_cleanup() -> None:
    job_id, attempt_id, lease_id, worker_id = uuid4(), uuid4(), uuid4(), uuid4()
    command = build_envelope(
        "processing.job.requested",
        "trace",
        str(job_id),
        "request",
        {
            "jobId": str(job_id),
            "datasetVersionId": str(uuid4()),
            "attemptId": str(attempt_id),
            "leaseId": str(lease_id),
            "workerId": str(worker_id),
            "attemptNumber": 1,
            "source": _source(),
            "operations": [{"type": "PROFILE_DATASET", "config": {}}],
        },
    )
    registry = CancellationRegistry()
    registry.request(job_id, "owner requested", attempt_id, lease_id)
    publisher = FakePublisher()
    cleanup: list[str] = []

    def runner(_command: object, _context: object) -> ProcessingRunResult:
        raise AssertionError("cancelled work must not enter the runner")

    executor = ProcessingJobExecutor(
        publisher,
        _validator(),
        registry,
        worker_id,
        runner,
        cleanup=lambda identity: cleanup.append(str(identity.job_id)),
    )

    await executor(command)

    assert cleanup == [str(job_id)]
    assert [message.message_type for message, _, _ in publisher.messages] == [
        "processing.job.started",
        "processing.job.cancelled",
    ]
    assert publisher.messages[-1][0].payload["reason"] == "owner requested"
    assert registry.is_cancelled(job_id, attempt_id, lease_id) is False


@pytest.mark.asyncio
async def test_executor_publishes_bounded_progress_before_success() -> None:
    job_id, attempt_id, lease_id, worker_id = uuid4(), uuid4(), uuid4(), uuid4()
    command = build_envelope(
        "processing.job.requested",
        "trace",
        str(job_id),
        "request",
        {
            "jobId": str(job_id),
            "datasetVersionId": str(uuid4()),
            "attemptId": str(attempt_id),
            "leaseId": str(lease_id),
            "workerId": str(worker_id),
            "attemptNumber": 1,
            "source": _source(),
            "operations": [{"type": "PROFILE_DATASET", "config": {}}],
        },
    )
    publisher = FakePublisher()

    def runner(_command: object, context: object) -> ProcessingRunResult:
        assert hasattr(context, "on_progress")
        context.on_progress(  # type: ignore[attr-defined]
            ProgressSnapshot(rows_processed=100, estimated_rows=200, fraction=0.5)
        )
        return ProcessingRunResult(
            (
                ArtifactDescriptor(
                    artifact_id=uuid4(),
                    kind="profile",
                    object_key="reports/fixture/attempt-1/profile.json",
                    size_bytes=2,
                    content_type="application/json",
                    checksum_sha256="0" * 64,
                ),
            )
        )

    executor = ProcessingJobExecutor(
        publisher, _validator(), CancellationRegistry(), worker_id, runner
    )

    await executor(command)

    assert [message.message_type for message, _, _ in publisher.messages] == [
        "processing.job.started",
        "processing.job.progressed",
        "processing.artifact.created",
        "processing.job.succeeded",
    ]
    assert publisher.messages[1][0].payload["progressPercent"] == 50.0
    assert publisher.messages[2][0].payload["kind"] == "profile"
    assert publisher.messages[3][0].payload["artifacts"] == [
        "reports/fixture/attempt-1/profile.json"
    ]


@pytest.mark.asyncio
async def test_progress_relay_keeps_latest_snapshots_with_a_bounded_buffer() -> None:
    relay = ProgressRelay(max_pending=2)
    relay.emit(ProgressSnapshot(rows_processed=1, estimated_rows=3, fraction=1 / 3))
    relay.emit(ProgressSnapshot(rows_processed=2, estimated_rows=3, fraction=2 / 3))
    relay.emit(ProgressSnapshot(rows_processed=3, estimated_rows=3, fraction=1.0, final=True))
    await asyncio.sleep(0)
    published: list[int] = []

    async def done() -> str:
        return "done"

    result = await relay.wait_for(
        asyncio.create_task(done()),
        lambda snapshot: _record_progress(published, snapshot.rows_processed),
    )

    assert result == "done"
    assert published == [2, 3]


@pytest.mark.asyncio
async def test_executor_reports_retryable_failure_after_artifact_upload_error() -> None:
    job_id, attempt_id, lease_id, worker_id = uuid4(), uuid4(), uuid4(), uuid4()
    command = build_envelope(
        "processing.job.requested",
        "trace",
        str(job_id),
        "request",
        {
            "jobId": str(job_id),
            "datasetVersionId": str(uuid4()),
            "attemptId": str(attempt_id),
            "leaseId": str(lease_id),
            "workerId": str(worker_id),
            "attemptNumber": 1,
            "source": _source(),
            "operations": [{"type": "PROFILE_DATASET", "config": {}}],
        },
    )

    def runner(_command: object, _context: object) -> ProcessingRunResult:
        raise OSError("artifact bucket unavailable")

    publisher = FakePublisher()
    executor = ProcessingJobExecutor(
        publisher, _validator(), CancellationRegistry(), worker_id, runner
    )

    await executor(command)

    assert [message.message_type for message, _, _ in publisher.messages] == [
        "processing.job.started",
        "processing.job.failed",
    ]
    assert publisher.messages[-1][0].payload["error"] == {
        "code": "WORKER_FAILURE",
        "message": "worker processing failed",
        "retryable": True,
    }


def test_active_runtime_installs_real_executor_and_health_only_remains_isolated() -> None:
    contracts_dir = Path(__file__).resolve().parents[2] / "contracts" / "json-schema"
    worker_id = uuid4()
    settings = Settings(contracts_dir=contracts_dir, worker_id=worker_id)
    active = build_runtime(settings)
    health_only = build_runtime(Settings(contracts_dir=contracts_dir, health_only=True))

    active_consumer = active.command_handler.__self__
    assert isinstance(active_consumer, JobCommandConsumer)
    assert isinstance(active_consumer._handler, ProcessingJobExecutor)
    assert isinstance(active.consumer, CompositeConsumer)
    job_consumer, cancellation_consumer = active.consumer._consumers
    assert isinstance(job_consumer, RabbitConsumer)
    assert isinstance(cancellation_consumer, RabbitConsumer)
    assert job_consumer._queue_name == settings.job_queue_name
    assert cancellation_consumer._queue_name == settings.cancellation_queue_name
    assert job_consumer._bindings[0].routing_key == settings.job_routing_key
    assert cancellation_consumer._bindings[0].routing_key == settings.cancellation_routing_key
    assert job_consumer._queue_arguments["x-dead-letter-routing-key"] == (
        settings.dead_letter_routing_key
    )
    assert cancellation_consumer._queue_arguments["x-dead-letter-routing-key"] == (
        settings.cancellation_dead_letter_routing_key
    )
    assert isinstance(active.storage, WorkerObjectStores)
    assert isinstance(health_only.command_handler, IdleCommandHandler)


def test_legacy_shared_consumers_are_explicitly_opt_in() -> None:
    contracts_dir = Path(__file__).resolve().parents[2] / "contracts" / "json-schema"
    default_runtime = build_runtime(Settings(contracts_dir=contracts_dir))
    compatible_runtime = build_runtime(
        Settings(contracts_dir=contracts_dir, legacy_shared_queue_compatibility=True)
    )

    assert isinstance(default_runtime.consumer, CompositeConsumer)
    assert len(default_runtime.consumer._consumers) == 2
    assert isinstance(compatible_runtime.consumer, CompositeConsumer)
    targeted_job, targeted_cancel, legacy_job, legacy_cancel = (
        compatible_runtime.consumer._consumers
    )
    assert targeted_job._queue_name.endswith(str(compatible_runtime.settings.worker_id))
    assert targeted_cancel._queue_name.endswith(str(compatible_runtime.settings.worker_id))
    assert legacy_job._queue_name == compatible_runtime.settings.broker_queue
    assert legacy_cancel._queue_name == compatible_runtime.settings.cancellation_queue
    assert legacy_job._bindings[0].routing_key == "processing.job.requested"
    assert legacy_cancel._bindings[0].routing_key == "processing.job.cancel-requested"


class RecordingConsumer:
    def __init__(self, events: list[str], stop_event: asyncio.Event) -> None:
        self.events = events
        self.stop_event = stop_event

    async def start(self, _handler: object) -> None:
        self.events.append("consumer-start")
        self.stop_event.set()

    async def stop(self) -> None:
        self.events.append("consumer-stop")


class HangingStopConsumer(RecordingConsumer):
    async def stop(self) -> None:
        self.events.append("consumer-stop-hanging")
        await asyncio.Event().wait()


class RecordingPublisher:
    def __init__(self, events: list[str]) -> None:
        self.events = events

    async def publish(self, _envelope: object, _routing_key: str, _headers: object = None) -> None:
        self.events.append("publisher-publish")

    async def close(self) -> None:
        self.events.append("publisher-close")


class RecordingStorage:
    def __init__(self, events: list[str]) -> None:
        self.events = events

    def ping(self) -> None:
        self.events.append("storage-ping")

    def close(self) -> None:
        self.events.append("storage-close")


class HangingStorage(RecordingStorage):
    def close(self) -> None:
        self.events.append("storage-close-hanging")
        threading.Event().wait()


class RecordingHealthServer:
    def __init__(self, events: list[str]) -> None:
        self.events = events

    def start(self) -> None:
        self.events.append("health-start")

    def stop(self) -> None:
        self.events.append("health-stop")


class HangingHealthServer(RecordingHealthServer):
    def stop(self) -> None:
        self.events.append("health-stop-hanging")
        threading.Event().wait()


class RecordingHeartbeat:
    def __init__(
        self,
        events: list[str],
        fail_status: WorkerStatus | None = None,
        fail_loop: bool = False,
    ) -> None:
        self.events = events
        self.fail_status = fail_status
        self.fail_loop = fail_loop

    async def publish_registration(self) -> None:
        self.events.append("registration")

    async def run(
        self,
        stop_event: asyncio.Event,
        _snapshot: Callable[[], tuple[WorkerStatus, tuple[str, ...]]],
    ) -> None:
        if self.fail_loop:
            raise OSError("heartbeat publisher failed")
        await stop_event.wait()
        self.events.append("heartbeat-loop-stopped")

    async def publish_heartbeat(
        self,
        status: WorkerStatus,
        _current_job_ids: tuple[str, ...],
        force: bool = False,
    ) -> bool:
        assert force is True
        self.events.append(f"heartbeat-{status.value.lower()}")
        if status is self.fail_status:
            raise OSError("broker unavailable")
        return True


def _recording_runtime(
    tmp_path: Path,
    events: list[str],
    stop_event: asyncio.Event,
    fail_status: WorkerStatus | None = None,
    fail_loop: bool = False,
    hang_on_stop: bool = False,
    hanging_sync_resource: str | None = None,
) -> WorkerRuntime:
    settings = Settings(contracts_dir=tmp_path, shutdown_timeout_seconds=1)
    metrics = WorkerMetrics()
    health = HealthState(settings.max_concurrency)
    consumer = (
        HangingStopConsumer(events, stop_event)
        if hang_on_stop
        else RecordingConsumer(events, stop_event)
    )
    return WorkerRuntime(
        settings,
        RecordingPublisher(events),  # type: ignore[arg-type]
        consumer,  # type: ignore[arg-type]
        HangingStorage(events) if hanging_sync_resource == "storage" else RecordingStorage(events),
        lambda _delivery: _return_disposition(MessageDisposition.REJECT),
        RecordingHeartbeat(events, fail_status, fail_loop),  # type: ignore[arg-type]
        WorkerLifecycle(),
        health,
        HangingHealthServer(events)
        if hanging_sync_resource == "health_server"
        else RecordingHealthServer(events),  # type: ignore[arg-type]
        metrics,
    )


@pytest.mark.asyncio
async def test_shutdown_stops_heartbeat_before_ordered_terminal_publications(
    tmp_path: Path,
) -> None:
    events: list[str] = []
    stop_event = asyncio.Event()

    await _recording_runtime(tmp_path, events, stop_event).run(stop_event)

    assert events.index("consumer-start") < events.index("registration")
    assert events.index("heartbeat-loop-stopped") < events.index("heartbeat-draining")
    assert events.index("heartbeat-draining") < events.index("consumer-stop")
    assert events.index("consumer-stop") < events.index("heartbeat-offline")
    assert events.index("heartbeat-offline") < events.index("publisher-close")


@pytest.mark.asyncio
@pytest.mark.parametrize("failed_status", [WorkerStatus.DRAINING, WorkerStatus.OFFLINE])
async def test_terminal_publication_failure_does_not_interrupt_shutdown(
    tmp_path: Path, failed_status: WorkerStatus
) -> None:
    events: list[str] = []
    stop_event = asyncio.Event()

    await _recording_runtime(tmp_path, events, stop_event, failed_status).run(stop_event)

    assert "consumer-stop" in events
    assert "publisher-close" in events
    assert "storage-close" in events
    assert "health-stop" in events


@pytest.mark.asyncio
async def test_heartbeat_loop_failure_stops_worker_and_releases_resources(tmp_path: Path) -> None:
    events: list[str] = []
    stop_event = asyncio.Event()

    with pytest.raises(OSError, match="heartbeat publisher failed"):
        await _recording_runtime(tmp_path, events, stop_event, fail_loop=True).run(stop_event)

    assert "consumer-stop" in events
    assert "publisher-close" in events
    assert "storage-close" in events
    assert "health-stop" in events


@pytest.mark.asyncio
async def test_shutdown_timeout_forces_remaining_resources_closed(tmp_path: Path) -> None:
    events: list[str] = []
    stop_event = asyncio.Event()

    with pytest.raises(RuntimeError, match="shutdown exceeded configured timeout"):
        await _recording_runtime(tmp_path, events, stop_event, hang_on_stop=True).run(stop_event)

    assert "publisher-close" in events
    assert "storage-close" in events
    assert "health-stop" in events


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("resource", "event"),
    [("storage", "storage-close-hanging"), ("health_server", "health-stop-hanging")],
)
async def test_blocking_sync_cleanup_respects_hard_shutdown_deadline(
    tmp_path: Path, resource: str, event: str
) -> None:
    events: list[str] = []
    stop_event = asyncio.Event()
    started_at = asyncio.get_running_loop().time()

    with pytest.raises(RuntimeError, match="shutdown exceeded configured timeout"):
        await _recording_runtime(tmp_path, events, stop_event, hanging_sync_resource=resource).run(
            stop_event
        )

    elapsed = asyncio.get_running_loop().time() - started_at
    assert elapsed < 1.25
    assert events.count(event) == 1


@pytest.mark.asyncio
async def test_heartbeat_is_schema_valid_and_throttled() -> None:
    current = datetime(2026, 8, 1, 12, 0, tzinfo=UTC)
    clock_value = [current]
    settings = Settings(
        contracts_dir=Path(__file__).resolve().parents[2] / "contracts" / "json-schema"
    )
    publisher = FakePublisher()
    controller = HeartbeatController(
        WorkerIdentity.from_settings(settings),
        publisher,
        _validator(),
        WorkerMetrics(),
        "pipeforge.events",
        15,
        clock=lambda: clock_value[0],
    )
    await controller.publish_registration()
    assert await controller.publish_heartbeat(
        WorkerStatus.READY,
        [str(uuid4()) for _ in range(200)],
        force=True,
    )
    clock_value[0] += timedelta(seconds=1)
    assert await controller.publish_heartbeat(WorkerStatus.READY, [], force=False) is False
    assert len(publisher.messages) == 2


@pytest.mark.asyncio
async def test_registration_refresh_preserves_instance_start_time() -> None:
    current = datetime(2026, 8, 1, 12, 0, tzinfo=UTC)
    clock_value = [current]
    settings = Settings(
        contracts_dir=Path(__file__).resolve().parents[2] / "contracts" / "json-schema"
    )
    publisher = FakePublisher()
    controller = HeartbeatController(
        WorkerIdentity.from_settings(settings),
        publisher,
        _validator(),
        WorkerMetrics(),
        "pipeforge.events",
        15,
        clock=lambda: clock_value[0],
    )

    await controller.publish_registration()
    clock_value[0] += timedelta(minutes=5)
    await controller.publish_registration()

    first = publisher.messages[0][0]
    refreshed = publisher.messages[1][0]
    assert first.payload["startedAt"] == refreshed.payload["startedAt"]
    assert first.message_id != refreshed.message_id


async def _return_disposition(disposition: MessageDisposition) -> MessageDisposition:
    await asyncio.sleep(0)
    return disposition


async def _record_progress(values: list[int], value: int) -> None:
    values.append(value)

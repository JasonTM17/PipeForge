import asyncio
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import pytest

from pipeforge_worker.config import Settings
from pipeforge_worker.consumers.job_commands import JobCommandConsumer, RejectingJobHandler
from pipeforge_worker.consumers.job_execution import ProcessingJobExecutor, ProcessingRunResult
from pipeforge_worker.contracts.envelope import build_envelope
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import Delivery, MessageDisposition, settle_delivery
from pipeforge_worker.observability.metrics import WorkerMetrics
from pipeforge_worker.processors.protocols import ProgressSnapshot
from pipeforge_worker.worker.cancellation import CancellationRegistry
from pipeforge_worker.worker.identity import HeartbeatController, WorkerIdentity, WorkerStatus


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
async def test_valid_command_is_durably_rejected_until_pipeline_exists() -> None:
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
            "operations": [{"type": "PROFILE_DATASET", "config": {}}],
        },
    )
    publisher = FakePublisher()

    def runner(_command: object, context: object) -> ProcessingRunResult:
        assert hasattr(context, "on_progress")
        context.on_progress(  # type: ignore[attr-defined]
            ProgressSnapshot(rows_processed=100, estimated_rows=200, fraction=0.5)
        )
        return ProcessingRunResult()

    executor = ProcessingJobExecutor(
        publisher, _validator(), CancellationRegistry(), worker_id, runner
    )

    await executor(command)

    assert [message.message_type for message, _, _ in publisher.messages] == [
        "processing.job.started",
        "processing.job.progressed",
        "processing.job.succeeded",
    ]
    assert publisher.messages[1][0].payload["progressPercent"] == 50.0


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


async def _return_disposition(disposition: MessageDisposition) -> MessageDisposition:
    await asyncio.sleep(0)
    return disposition

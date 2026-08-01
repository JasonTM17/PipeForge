import asyncio
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import uuid4

import pytest

from pipeforge_worker.config import Settings
from pipeforge_worker.consumers.job_commands import JobCommandConsumer, RejectingJobHandler
from pipeforge_worker.contracts.validator import ContractValidator
from pipeforge_worker.messaging.protocols import Delivery, MessageDisposition, settle_delivery
from pipeforge_worker.observability.metrics import WorkerMetrics
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

import asyncio
from collections.abc import Callable
from typing import Any

import pytest

from pipeforge_worker.messaging.protocols import MessageDisposition
from pipeforge_worker.messaging.rabbitmq import CompositeConsumer, RabbitBinding, RabbitConsumer


class FakeQueue:
    def __init__(self) -> None:
        self.bindings: list[tuple[object, str]] = []
        self.consumer: Callable[[object], object] | None = None

    async def bind(self, exchange: object, routing_key: str) -> None:
        self.bindings.append((exchange, routing_key))

    async def consume(self, callback: Callable[[object], object], no_ack: bool) -> str:
        assert no_ack is False
        self.consumer = callback
        return "consumer-tag"


class FakeChannel:
    def __init__(self) -> None:
        self.queue = FakeQueue()
        self.exchanges: list[tuple[str, object, bool, object]] = []

    async def set_qos(self, prefetch_count: int) -> None:
        assert prefetch_count == 2

    async def declare_queue(
        self, queue_name: str, *, durable: bool, arguments: object
    ) -> FakeQueue:
        assert queue_name == "processing.jobs.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
        assert durable is True
        assert arguments == {"x-dead-letter-routing-key": "processing.jobs.dlq"}
        return self.queue

    async def declare_exchange(
        self, exchange_name: str, exchange_type: object, *, durable: bool
    ) -> object:
        exchange = object()
        self.exchanges.append((exchange_name, exchange_type, durable, exchange))
        return exchange


class FakeConnection:
    is_closed = False

    def __init__(self) -> None:
        self.channel_instance = FakeChannel()

    async def channel(self) -> FakeChannel:
        return self.channel_instance


@pytest.mark.asyncio
async def test_consumer_declares_only_the_explicit_worker_binding(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    connection = FakeConnection()

    async def connect_robust(_url: str, *, reconnect_interval: float) -> FakeConnection:
        assert reconnect_interval == 2.0
        return connection

    monkeypatch.setattr(
        "pipeforge_worker.messaging.rabbitmq.aio_pika.connect_robust",
        connect_robust,
    )
    routing_key = "processing.job.requested.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    consumer = RabbitConsumer(
        "amqp://example",
        "processing.jobs.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
        2,
        {"x-dead-letter-routing-key": "processing.jobs.dlq"},
        bindings=(RabbitBinding("pipeforge.commands", routing_key),),
    )

    async def handler(_delivery: Any) -> MessageDisposition:
        return MessageDisposition.ACK

    await consumer.start(handler)

    channel = connection.channel_instance
    assert [(name, durable) for name, _, durable, _ in channel.exchanges] == [
        ("pipeforge.commands", True)
    ]
    assert channel.queue.bindings == [(channel.exchanges[0][3], routing_key)]
    assert all(
        "ffffffff-ffff-4fff-8fff-ffffffffffff" not in key for _, key in channel.queue.bindings
    )


@pytest.mark.parametrize(
    ("exchange_name", "routing_key"),
    [
        ("", "processing.job.requested.worker"),
        ("pipeforge.commands", "\n"),
        ("x" * 256, "processing.job.requested.worker"),
    ],
)
def test_binding_rejects_unsafe_names(exchange_name: str, routing_key: str) -> None:
    with pytest.raises(ValueError):
        RabbitBinding(exchange_name, routing_key)


class OrderedStopConsumer:
    def __init__(self, name: str, events: list[str], release: asyncio.Event | None = None) -> None:
        self.name = name
        self.events = events
        self.release = release

    async def start(self, _handler: Any) -> None:
        return

    async def stop(self) -> None:
        self.events.append(f"{self.name}-stop-start")
        if self.release is not None:
            await self.release.wait()
        self.events.append(f"{self.name}-stop-done")


@pytest.mark.asyncio
async def test_composite_stops_job_intake_before_cancellation_intake() -> None:
    events: list[str] = []
    release_jobs = asyncio.Event()
    job = OrderedStopConsumer("job", events, release_jobs)
    cancellation = OrderedStopConsumer("cancellation", events)
    composite = CompositeConsumer(
        (job, cancellation),
        ((job,), (cancellation,)),
    )

    stopping = asyncio.create_task(composite.stop())
    while "job-stop-start" not in events:
        await asyncio.sleep(0)

    assert "cancellation-stop-start" not in events
    release_jobs.set()
    await stopping
    assert events == [
        "job-stop-start",
        "job-stop-done",
        "cancellation-stop-start",
        "cancellation-stop-done",
    ]

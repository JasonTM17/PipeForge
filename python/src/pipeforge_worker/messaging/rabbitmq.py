"""Aio-pika transport with publisher confirms and manual delivery settlement."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any, cast

import aio_pika
from aio_pika import DeliveryMode, ExchangeType, Message

from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.messaging.protocols import (
    Consumer,
    Delivery,
    MessageHandler,
    Publisher,
    settle_delivery,
)


def _validated_amqp_name(value: str, label: str) -> str:
    normalized = value.strip()
    if not normalized or any(ord(char) < 32 for char in normalized):
        raise ValueError(f"{label} must be non-empty and free of control characters")
    if len(normalized.encode("utf-8")) > 255:
        raise ValueError(f"{label} must be at most 255 UTF-8 bytes")
    return normalized


@dataclass(frozen=True)
class RabbitBinding:
    """One durable topic-exchange binding for a consumer queue."""

    exchange_name: str
    routing_key: str

    def __post_init__(self) -> None:
        object.__setattr__(
            self,
            "exchange_name",
            _validated_amqp_name(self.exchange_name, "binding exchange name"),
        )
        object.__setattr__(
            self,
            "routing_key",
            _validated_amqp_name(self.routing_key, "binding routing key"),
        )


class RabbitPublisher(Publisher):
    def __init__(self, url: str, exchange_name: str, reconnect_delay_seconds: float = 2.0) -> None:
        self._url = url
        self._exchange_name = exchange_name
        self._reconnect_delay_seconds = reconnect_delay_seconds
        self._connection: Any = None
        self._channel: Any = None
        self._exchange: Any = None
        self._lock = asyncio.Lock()
        self._logger = logging.getLogger(__name__)

    async def connect(self) -> None:
        if self._connection is not None and not self._connection.is_closed:
            return
        async with self._lock:
            if self._connection is not None and not self._connection.is_closed:
                return
            self._connection = await aio_pika.connect_robust(
                self._url,
                reconnect_interval=self._reconnect_delay_seconds,
            )
            self._channel = await self._connection.channel(publisher_confirms=True)
            self._exchange = await self._channel.declare_exchange(
                self._exchange_name,
                ExchangeType.TOPIC,
                durable=True,
            )
            self._logger.info("RabbitMQ publisher connected")

    async def publish(
        self, envelope: Envelope, routing_key: str, headers: Mapping[str, object] | None = None
    ) -> None:
        if not routing_key.strip():
            raise ValueError("routing key is required")
        await self.connect()
        message = Message(
            body=envelope.to_bytes(),
            delivery_mode=DeliveryMode.PERSISTENT,
            content_type="application/json",
            message_id=str(envelope.message_id),
            type=envelope.message_type,
            headers=cast(Any, dict(headers or {})),
        )
        await self._exchange.publish(message, routing_key=routing_key)

    async def close(self) -> None:
        if self._connection is not None and not self._connection.is_closed:
            await self._connection.close()
        self._connection = None
        self._channel = None
        self._exchange = None


class RabbitDelivery(Delivery):
    def __init__(self, message: Any) -> None:
        self._message = message
        self.body = bytes(message.body)
        self.headers = dict(message.headers or {})
        self.message_type = str(message.type or "unknown")

    async def ack(self) -> None:
        await self._message.ack()

    async def reject(self, requeue: bool = False) -> None:
        await self._message.reject(requeue=requeue)


class RabbitConsumer(Consumer):
    def __init__(
        self,
        url: str,
        queue_name: str,
        prefetch_count: int,
        queue_arguments: Mapping[str, object] | None = None,
        reconnect_delay_seconds: float = 2.0,
        bindings: tuple[RabbitBinding, ...] = (),
    ) -> None:
        self._url = url
        self._queue_name = _validated_amqp_name(queue_name, "queue name")
        if not 1 <= prefetch_count <= 1000:
            raise ValueError("prefetch count must be between 1 and 1000")
        if not 0.1 <= reconnect_delay_seconds <= 300:
            raise ValueError("reconnect delay must be between 0.1 and 300 seconds")
        self._prefetch_count = prefetch_count
        self._queue_arguments = dict(queue_arguments or {})
        self._reconnect_delay_seconds = reconnect_delay_seconds
        self._bindings = bindings
        self._connection: Any = None
        self._channel: Any = None
        self._queue: Any = None
        self._consumer_tag: str | None = None
        self._in_flight: set[asyncio.Task[None]] = set()
        self._logger = logging.getLogger(__name__)

    async def start(self, handler: MessageHandler) -> None:
        self._connection = await aio_pika.connect_robust(
            self._url,
            reconnect_interval=self._reconnect_delay_seconds,
        )
        self._channel = await self._connection.channel()
        await self._channel.set_qos(prefetch_count=self._prefetch_count)
        self._queue = await self._channel.declare_queue(
            self._queue_name,
            durable=True,
            arguments=cast(Any, self._queue_arguments),
        )
        for binding in self._bindings:
            exchange = await self._channel.declare_exchange(
                binding.exchange_name,
                ExchangeType.TOPIC,
                durable=True,
            )
            await self._queue.bind(exchange, routing_key=binding.routing_key)
        self._consumer_tag = await self._queue.consume(
            lambda message: self._settle(message, handler), no_ack=False
        )
        self._logger.info("RabbitMQ consumer started")

    async def _settle(self, message: Any, handler: MessageHandler) -> None:
        delivery = RabbitDelivery(message)
        task = asyncio.current_task()
        if task is not None:
            self._in_flight.add(task)
        try:
            await settle_delivery(delivery, handler)
        except Exception:
            self._logger.exception("worker message handling failed; sending to dead-letter path")
            await delivery.reject(requeue=False)
        finally:
            if task is not None:
                self._in_flight.discard(task)

    async def stop(self) -> None:
        if self._queue is not None and self._consumer_tag is not None:
            await self._queue.cancel(self._consumer_tag)
        if self._in_flight:
            await asyncio.gather(*tuple(self._in_flight), return_exceptions=True)
        if self._connection is not None and not self._connection.is_closed:
            await self._connection.close()
        self._connection = None
        self._channel = None
        self._queue = None
        self._consumer_tag = None
        self._in_flight.clear()


class CompositeConsumer(Consumer):
    """Start and drain multiple queues behind the runtime's one consumer boundary."""

    def __init__(
        self,
        consumers: tuple[Consumer, ...],
        shutdown_groups: tuple[tuple[Consumer, ...], ...] | None = None,
    ) -> None:
        if not consumers:
            raise ValueError("at least one consumer is required")
        self._consumers = consumers
        self._shutdown_groups = shutdown_groups or tuple(
            (consumer,) for consumer in reversed(consumers)
        )
        grouped = tuple(consumer for group in self._shutdown_groups for consumer in group)
        if len(grouped) != len(consumers) or {id(item) for item in grouped} != {
            id(item) for item in consumers
        }:
            raise ValueError("shutdown groups must contain every consumer exactly once")

    async def start(self, handler: MessageHandler) -> None:
        started: list[Consumer] = []
        try:
            for consumer in self._consumers:
                await consumer.start(handler)
                started.append(consumer)
        except Exception:
            for consumer in reversed(started):
                await consumer.stop()
            raise

    async def stop(self) -> None:
        failures: list[Exception] = []
        for group in self._shutdown_groups:
            results = await asyncio.gather(
                *(consumer.stop() for consumer in group), return_exceptions=True
            )
            failures.extend(result for result in results if isinstance(result, Exception))
        if failures:
            raise ExceptionGroup("one or more RabbitMQ consumers failed to stop", failures)

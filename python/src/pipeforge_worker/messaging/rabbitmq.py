"""Aio-pika transport with publisher confirms and manual delivery settlement."""

from __future__ import annotations

import logging
from collections.abc import Mapping
from typing import Any, cast

import aio_pika
from aio_pika import DeliveryMode, ExchangeType, Message

from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.messaging.protocols import (
    Consumer,
    Delivery,
    MessageDisposition,
    MessageHandler,
    Publisher,
)


class RabbitPublisher(Publisher):
    def __init__(self, url: str, exchange_name: str, reconnect_delay_seconds: float = 2.0) -> None:
        self._url = url
        self._exchange_name = exchange_name
        self._reconnect_delay_seconds = reconnect_delay_seconds
        self._connection: Any = None
        self._channel: Any = None
        self._exchange: Any = None
        self._lock = __import__("asyncio").Lock()
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
        reconnect_delay_seconds: float = 2.0,
    ) -> None:
        self._url = url
        self._queue_name = queue_name
        self._prefetch_count = prefetch_count
        self._reconnect_delay_seconds = reconnect_delay_seconds
        self._connection: Any = None
        self._channel: Any = None
        self._queue: Any = None
        self._consumer_tag: str | None = None
        self._logger = logging.getLogger(__name__)

    async def start(self, handler: MessageHandler) -> None:
        self._connection = await aio_pika.connect_robust(
            self._url,
            reconnect_interval=self._reconnect_delay_seconds,
        )
        self._channel = await self._connection.channel()
        await self._channel.set_qos(prefetch_count=self._prefetch_count)
        self._queue = await self._channel.declare_queue(self._queue_name, durable=True)
        self._consumer_tag = await self._queue.consume(
            lambda message: self._settle(message, handler), no_ack=False
        )
        self._logger.info("RabbitMQ consumer started")

    async def _settle(self, message: Any, handler: MessageHandler) -> None:
        delivery = RabbitDelivery(message)
        try:
            disposition = await handler(delivery)
            if disposition is MessageDisposition.ACK:
                await delivery.ack()
            else:
                await delivery.reject(requeue=False)
        except Exception:
            self._logger.exception("worker message handling failed; sending to dead-letter path")
            await delivery.reject(requeue=False)

    async def stop(self) -> None:
        if self._queue is not None and self._consumer_tag is not None:
            await self._queue.cancel(self._consumer_tag)
        if self._connection is not None and not self._connection.is_closed:
            await self._connection.close()
        self._connection = None
        self._channel = None
        self._queue = None
        self._consumer_tag = None

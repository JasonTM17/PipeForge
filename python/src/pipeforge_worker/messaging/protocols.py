"""Small transport protocols that keep worker logic independent of aio-pika."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, Mapping
from enum import StrEnum
from typing import Protocol

from pipeforge_worker.contracts.envelope import Envelope


class MessageDisposition(StrEnum):
    ACK = "ack"
    REJECT = "reject"


class Delivery(Protocol):
    body: bytes
    headers: Mapping[str, object]
    message_type: str

    async def ack(self) -> None: ...

    async def reject(self, requeue: bool = False) -> None: ...


MessageHandler = Callable[[Delivery], Awaitable[MessageDisposition]]


class Publisher(Protocol):
    async def publish(
        self, envelope: Envelope, routing_key: str, headers: Mapping[str, object] | None = None
    ) -> None: ...

    async def close(self) -> None: ...


class Consumer(Protocol):
    async def start(self, handler: MessageHandler) -> None: ...

    async def stop(self) -> None: ...


class PermanentMessageError(RuntimeError):
    """The message is malformed or unsupported and belongs in the DLQ."""


class TransientMessageError(RuntimeError):
    """A bounded transport/storage failure that must be durably classified."""


async def settle_delivery(delivery: Delivery, handler: MessageHandler) -> None:
    """Run a handler before acknowledging; rejection always routes to the broker DLX."""

    disposition = await handler(delivery)
    if disposition is MessageDisposition.ACK:
        await delivery.ack()
    else:
        await delivery.reject(requeue=False)

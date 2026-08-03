"""Bounded cross-thread progress relay for streamed worker events."""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable

from pipeforge_worker.processors.protocols import ProgressSnapshot


class ProgressRelay:
    """Keep progress memory bounded while a synchronous pipeline runs in a thread."""

    def __init__(self, max_pending: int = 32) -> None:
        self._loop = asyncio.get_running_loop()
        self._queue: asyncio.Queue[ProgressSnapshot] = asyncio.Queue(maxsize=max_pending)

    def emit(self, snapshot: ProgressSnapshot) -> None:
        self._loop.call_soon_threadsafe(self._offer, snapshot)

    def _offer(self, snapshot: ProgressSnapshot) -> None:
        if self._queue.full():
            self._queue.get_nowait()
        self._queue.put_nowait(snapshot)

    async def wait_for(
        self,
        runner: asyncio.Task[object],
        publish: Callable[[ProgressSnapshot], Awaitable[None]],
    ) -> object:
        while not runner.done():
            try:
                snapshot = await asyncio.wait_for(self._queue.get(), timeout=0.1)
            except TimeoutError:
                continue
            await publish(snapshot)
        result = await runner
        await asyncio.sleep(0)
        while not self._queue.empty():
            await publish(self._queue.get_nowait())
        return result

"""Dependency-injected worker runtime and graceful shutdown ordering."""

from __future__ import annotations

import asyncio
import logging
import sys
import threading
from collections.abc import Callable
from typing import Protocol

from pipeforge_worker.config import Settings
from pipeforge_worker.messaging.protocols import Consumer, MessageHandler, Publisher
from pipeforge_worker.observability.health import HealthServer, HealthState
from pipeforge_worker.observability.metrics import WorkerMetrics
from pipeforge_worker.worker.identity import HeartbeatController, WorkerStatus
from pipeforge_worker.worker.lifecycle import WorkerLifecycle


class PingingObjectStore(Protocol):
    def ping(self) -> None: ...

    def close(self) -> None: ...


class WorkerRuntime:
    def __init__(
        self,
        settings: Settings,
        publisher: Publisher,
        consumer: Consumer,
        storage: PingingObjectStore,
        command_handler: MessageHandler,
        heartbeat: HeartbeatController | None,
        lifecycle: WorkerLifecycle,
        health: HealthState,
        health_server: HealthServer,
        metrics: WorkerMetrics,
    ) -> None:
        self.settings = settings
        self.publisher = publisher
        self.consumer = consumer
        self.storage = storage
        self.command_handler = command_handler
        self.heartbeat = heartbeat
        self.lifecycle = lifecycle
        self.health = health
        self.health_server = health_server
        self.metrics = metrics
        self._logger = logging.getLogger(__name__)

    async def run(self, stop_event: asyncio.Event) -> None:
        heartbeat_stop = asyncio.Event()
        heartbeat_task: asyncio.Task[None] | None = None
        stop_wait_task: asyncio.Task[bool] | None = None
        self.health_server.start()
        self._set_status(WorkerStatus.STARTING)
        try:
            if self.settings.health_only:
                self._set_status(WorkerStatus.READY)
                await stop_event.wait()
                return

            await asyncio.to_thread(self.storage.ping)
            self.health.set_dependency("object_storage", True)
            if self.heartbeat is None:
                raise RuntimeError("heartbeat controller is required for active worker mode")
            await self.consumer.start(self.command_handler)
            await self.heartbeat.publish_registration()
            self.health.set_dependency("broker", True)
            self._set_status(WorkerStatus.READY)
            heartbeat_task = asyncio.create_task(
                self.heartbeat.run(heartbeat_stop, self._heartbeat_snapshot),
                name="worker-heartbeat",
            )
            stop_wait_task = asyncio.create_task(stop_event.wait(), name="worker-stop-wait")
            done, _ = await asyncio.wait(
                {heartbeat_task, stop_wait_task}, return_when=asyncio.FIRST_COMPLETED
            )
            if heartbeat_task in done:
                await heartbeat_task
                if not stop_event.is_set():
                    raise RuntimeError("worker heartbeat loop stopped unexpectedly")
        except Exception:
            self._set_status(WorkerStatus.UNHEALTHY)
            self._logger.exception("worker runtime failed")
            raise
        finally:
            active_error = sys.exc_info()[1]
            if stop_wait_task is not None and not stop_wait_task.done():
                stop_wait_task.cancel()
                await asyncio.gather(stop_wait_task, return_exceptions=True)
            heartbeat_stop.set()
            shutdown_error = await self._shutdown(heartbeat_task)
            if shutdown_error is not None and active_error is None:
                raise shutdown_error

    async def _shutdown(self, heartbeat_task: asyncio.Task[None] | None) -> Exception | None:
        loop = asyncio.get_running_loop()
        shutdown_deadline = loop.time() + self.settings.shutdown_timeout_seconds
        graceful_deadline = loop.time() + self.settings.shutdown_timeout_seconds * 0.75
        started_sync_closers: set[str] = set()
        try:
            async with asyncio.timeout_at(graceful_deadline):
                if heartbeat_task is not None:
                    await asyncio.gather(heartbeat_task, return_exceptions=True)
                self._set_status(WorkerStatus.DRAINING)
                await self._publish_terminal_status(WorkerStatus.DRAINING)
                await self.consumer.stop()
                self._set_status(WorkerStatus.OFFLINE)
                await self._publish_terminal_status(WorkerStatus.OFFLINE)
                await self.publisher.close()
                await self._close_sync_resource(
                    "storage", self.storage.close, started_sync_closers, graceful_deadline
                )
                await self._close_sync_resource(
                    "health_server",
                    self.health_server.stop,
                    started_sync_closers,
                    graceful_deadline,
                )
            return None
        except TimeoutError:
            self._logger.error(
                "worker graceful shutdown exceeded reserved deadline",
                extra={"shutdown_timeout_seconds": self.settings.shutdown_timeout_seconds},
            )
            await self._force_close_after_timeout(shutdown_deadline, started_sync_closers)
            return RuntimeError("worker shutdown exceeded configured timeout")
        except Exception as error:
            self._logger.exception("worker shutdown failed; forcing remaining resources closed")
            await self._force_close_after_timeout(shutdown_deadline, started_sync_closers)
            return error

    async def _force_close_after_timeout(
        self, shutdown_deadline: float, started_sync_closers: set[str]
    ) -> None:
        for label, operation in (
            ("consumer", self.consumer.stop),
            ("publisher", self.publisher.close),
        ):
            try:
                async with asyncio.timeout_at(shutdown_deadline):
                    await operation()
            except Exception:
                self._logger.exception(
                    "forced worker resource close failed", extra={"resource": label}
                )
        for label, sync_operation in (
            ("storage", self.storage.close),
            ("health_server", self.health_server.stop),
        ):
            try:
                await self._close_sync_resource(
                    label, sync_operation, started_sync_closers, shutdown_deadline
                )
            except Exception:
                self._logger.exception(
                    "forced worker resource close failed", extra={"resource": label}
                )

    async def _close_sync_resource(
        self,
        label: str,
        operation: Callable[[], None],
        started_sync_closers: set[str],
        deadline: float,
    ) -> None:
        if label in started_sync_closers:
            return
        started_sync_closers.add(label)
        loop = asyncio.get_running_loop()
        completed: asyncio.Future[Exception | None] = loop.create_future()

        def run() -> None:
            error: Exception | None = None
            try:
                operation()
            except Exception as caught:
                error = caught
            try:
                loop.call_soon_threadsafe(complete, error)
            except RuntimeError:
                pass

        def complete(error: Exception | None) -> None:
            if not completed.done():
                completed.set_result(error)

        threading.Thread(target=run, name=f"pipeforge-close-{label}", daemon=True).start()
        async with asyncio.timeout_at(deadline):
            error = await completed
        if error is not None:
            raise error

    async def _publish_terminal_status(self, status: WorkerStatus) -> None:
        if self.heartbeat is None:
            return
        try:
            current_job_ids = self.lifecycle.snapshot().current_job_ids
            await self.heartbeat.publish_heartbeat(status, current_job_ids, force=True)
        except Exception:
            self._logger.exception(
                "failed to publish terminal worker status; continuing shutdown",
                extra={"worker_status": status.value},
            )

    def _heartbeat_snapshot(self) -> tuple[WorkerStatus, tuple[str, ...]]:
        snapshot = self.lifecycle.snapshot()
        self.health.set_current_jobs(snapshot.current_job_ids)
        return snapshot.status, snapshot.current_job_ids

    def _set_status(self, status: WorkerStatus) -> None:
        self.lifecycle.set_status(status)
        self.health.set_status(status.value)
        self.metrics.set_status(status.value)

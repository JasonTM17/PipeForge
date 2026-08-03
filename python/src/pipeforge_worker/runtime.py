"""Dependency-injected worker runtime and graceful shutdown ordering."""

from __future__ import annotations

import asyncio
import logging
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
            await self.heartbeat.publish_registration()
            self.health.set_dependency("broker", True)
            await self.consumer.start(self.command_handler)
            self._set_status(WorkerStatus.READY)
            heartbeat_task = asyncio.create_task(
                self.heartbeat.run(heartbeat_stop, self._heartbeat_snapshot),
                name="worker-heartbeat",
            )
            await stop_event.wait()
        except Exception:
            self._set_status(WorkerStatus.UNHEALTHY)
            self._logger.exception("worker runtime failed")
            raise
        finally:
            self._set_status(WorkerStatus.DRAINING)
            await self.consumer.stop()
            heartbeat_stop.set()
            if heartbeat_task is not None:
                await asyncio.gather(heartbeat_task, return_exceptions=True)
            await self.publisher.close()
            self.storage.close()
            self._set_status(WorkerStatus.OFFLINE)
            self.health_server.stop()

    def _heartbeat_snapshot(self) -> tuple[WorkerStatus, tuple[str, ...]]:
        snapshot = self.lifecycle.snapshot()
        self.health.set_current_jobs(snapshot.current_job_ids)
        return snapshot.status, snapshot.current_job_ids

    def _set_status(self, status: WorkerStatus) -> None:
        self.lifecycle.set_status(status)
        self.health.set_status(status.value)
        self.metrics.set_status(status.value)

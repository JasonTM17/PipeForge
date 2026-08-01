"""Bounded in-memory worker lifecycle state used by heartbeats and health."""

from __future__ import annotations

from dataclasses import dataclass

from pipeforge_worker.worker.identity import WorkerStatus


@dataclass(frozen=True, slots=True)
class LifecycleSnapshot:
    status: WorkerStatus
    current_job_ids: tuple[str, ...]


class WorkerLifecycle:
    def __init__(self) -> None:
        self._status = WorkerStatus.STARTING
        self._current_job_ids: set[str] = set()

    def set_status(self, status: WorkerStatus) -> None:
        self._status = status

    def add_job(self, job_id: str) -> None:
        if len(self._current_job_ids) < 128:
            self._current_job_ids.add(job_id)

    def remove_job(self, job_id: str) -> None:
        self._current_job_ids.discard(job_id)

    def snapshot(self) -> LifecycleSnapshot:
        return LifecycleSnapshot(self._status, tuple(sorted(self._current_job_ids)))

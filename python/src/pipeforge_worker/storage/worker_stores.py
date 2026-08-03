"""Dataset and artifact bucket lifecycle for the active worker."""

from __future__ import annotations

from dataclasses import dataclass

from pipeforge_worker.storage.minio import MinioObjectStore


@dataclass(slots=True)
class WorkerObjectStores:
    source: MinioObjectStore
    artifacts: MinioObjectStore

    def ping(self) -> None:
        self.source.ping()
        self.artifacts.ping()

    def close(self) -> None:
        try:
            self.source.close()
        finally:
            self.artifacts.close()

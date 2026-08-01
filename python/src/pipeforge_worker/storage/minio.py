"""MinIO adapter with streaming I/O and attempt-scoped key validation."""

from __future__ import annotations

import re
from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Any, BinaryIO, Protocol, cast
from uuid import UUID

from minio import Minio


class ObjectStore(Protocol):
    @contextmanager
    def download_stream(self, key: str) -> Iterator[BinaryIO]: ...

    def upload_stream(
        self, key: str, stream: BinaryIO, size: int, content_type: str
    ) -> StoredObject: ...

    def close(self) -> None: ...


@dataclass(frozen=True, slots=True)
class StoredObject:
    key: str
    etag: str
    size: int
    content_type: str


class MinioObjectStore(ObjectStore):
    def __init__(
        self, endpoint: str, access_key: str, secret_key: str, secure: bool, bucket: str
    ) -> None:
        self._bucket = _safe_bucket(bucket)
        self._client = Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure)

    @contextmanager
    def download_stream(self, key: str) -> Iterator[BinaryIO]:
        response: Any = self._client.get_object(self._bucket, _safe_object_key(key))
        try:
            yield cast(BinaryIO, response)
        finally:
            response.close()
            response.release_conn()

    def upload_stream(
        self, key: str, stream: BinaryIO, size: int, content_type: str
    ) -> StoredObject:
        if size < 0:
            raise ValueError("stream size must be non-negative")
        result = self._client.put_object(
            self._bucket,
            _safe_object_key(key),
            stream,
            length=size,
            content_type=content_type,
        )
        return StoredObject(key=key, etag=result.etag or "", size=size, content_type=content_type)

    def head(self, key: str) -> StoredObject:
        result: Any = self._client.stat_object(self._bucket, _safe_object_key(key))
        if result.size is None:
            raise ValueError("object storage returned an unknown object size")
        return StoredObject(
            key=key,
            etag=result.etag or "",
            size=int(result.size),
            content_type=result.content_type or "application/octet-stream",
        )

    def delete(self, key: str) -> None:
        self._client.remove_object(self._bucket, _safe_object_key(key))

    def close(self) -> None:
        return


def artifact_key(job_id: UUID, attempt_number: int, filename: str) -> str:
    return _scoped_key("reports", job_id, attempt_number, filename)


def quarantine_key(job_id: UUID, attempt_number: int, filename: str) -> str:
    return _scoped_key("quarantine", job_id, attempt_number, filename)


def _scoped_key(prefix: str, job_id: UUID, attempt_number: int, filename: str) -> str:
    if attempt_number < 1:
        raise ValueError("attempt number must be positive")
    if not _SAFE_FILENAME.fullmatch(filename):
        raise ValueError("filename contains unsafe path characters")
    return f"{prefix}/{job_id}/attempt-{attempt_number}/{filename}"


_SAFE_FILENAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")


def _safe_bucket(bucket: str) -> str:
    if not re.fullmatch(r"[a-z0-9][a-z0-9.-]{2,62}", bucket):
        raise ValueError("invalid object-storage bucket")
    return bucket


def _safe_object_key(key: str) -> str:
    if not key or len(key) > 1024 or "\\" in key or ".." in key or key.startswith("/"):
        raise ValueError("invalid object-storage key")
    return key

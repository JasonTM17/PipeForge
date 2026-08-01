"""Small stream helpers that preserve bounded reads for object-store bodies."""

from __future__ import annotations

import io
import tempfile
from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from typing import Any, BinaryIO, cast

from pipeforge_worker.readers.models import DatasetLimitError


class PrefixedBinaryStream(io.RawIOBase):
    """Expose already-read sniff bytes before continuing with a non-seekable body."""

    def __init__(self, prefix: bytes, source: BinaryIO) -> None:
        self._prefix = memoryview(prefix)
        self._offset = 0
        self._source = source

    def readable(self) -> bool:
        return True

    def seekable(self) -> bool:
        return False

    def readinto(self, buffer: Any) -> int:
        target = memoryview(buffer)
        copied = 0
        if self._offset < len(self._prefix):
            count = min(len(target), len(self._prefix) - self._offset)
            target[:count] = self._prefix[self._offset : self._offset + count]
            self._offset += count
            copied = count
        if copied == len(target):
            return copied
        remainder = self._source.read(len(target) - copied)
        if not remainder:
            return copied
        target[copied : copied + len(remainder)] = remainder
        return copied + len(remainder)

    def close(self) -> None:
        super().close()


class LimitedBinaryStream(io.RawIOBase):
    """Count bytes from a source and fail before a configured bound is crossed."""

    def __init__(self, source: BinaryIO, max_bytes: int) -> None:
        self._source = source
        self._max_bytes = max_bytes
        self.count = 0

    def readable(self) -> bool:
        return True

    def seekable(self) -> bool:
        return False

    def readinto(self, buffer: Any) -> int:
        target = memoryview(buffer)
        remaining = self._max_bytes - self.count
        if remaining <= 0:
            probe = self._source.read(1)
            if probe:
                raise DatasetLimitError("dataset exceeds the configured byte limit")
            return 0
        requested = min(len(target), remaining + 1)
        value = self._source.read(requested)
        if not value:
            return 0
        if self.count + len(value) > self._max_bytes:
            raise DatasetLimitError("dataset exceeds the configured byte limit")
        target[: len(value)] = value
        self.count += len(value)
        return len(value)

    def close(self) -> None:
        super().close()


@dataclass
class PreparedStream:
    prefix: bytes
    stream: BinaryIO
    owns_stream: bool


@contextmanager
def prepare_stream(source: BinaryIO, sniff_bytes: int) -> Iterator[PreparedStream]:
    prefix = source.read(sniff_bytes)
    if not isinstance(prefix, bytes):
        prefix = bytes(prefix)
    try:
        position = source.tell()
        source.seek(position - len(prefix))
    except (AttributeError, OSError, io.UnsupportedOperation):
        buffered = io.BufferedReader(PrefixedBinaryStream(prefix, source))
        prepared = PreparedStream(prefix=prefix, stream=buffered, owns_stream=True)
    else:
        prepared = PreparedStream(prefix=prefix, stream=source, owns_stream=False)
    try:
        yield prepared
    finally:
        if prepared.owns_stream:
            prepared.stream.close()


@contextmanager
def seekable_copy(source: BinaryIO, max_bytes: int, memory_limit: int) -> Iterator[BinaryIO]:
    if source.seekable():
        try:
            source.seek(0, 2)
            size = source.tell()
        except (AttributeError, OSError, io.UnsupportedOperation):
            pass
        else:
            if size > max_bytes:
                raise DatasetLimitError("dataset exceeds the configured byte limit")
            source.seek(0)
            yield source
            return

    with tempfile.SpooledTemporaryFile(max_size=memory_limit, mode="w+b") as target:
        total = 0
        while True:
            chunk = source.read(min(1024 * 1024, max_bytes - total + 1))
            if not chunk:
                break
            total += len(chunk)
            if total > max_bytes:
                raise DatasetLimitError("dataset exceeds the configured byte limit")
            target.write(chunk)
        target.seek(0)
        yield cast(BinaryIO, target)


@contextmanager
def limited_stream(
    source: BinaryIO, max_bytes: int
) -> Iterator[tuple[BinaryIO, LimitedBinaryStream]]:
    limiter = LimitedBinaryStream(source, max_bytes)
    buffered = io.BufferedReader(limiter)
    try:
        yield buffered, limiter
    finally:
        buffered.close()

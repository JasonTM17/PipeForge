"""Reader data structures and bounded input policies."""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import AbstractContextManager
from dataclasses import dataclass, field
from enum import StrEnum
from typing import BinaryIO, Protocol

import polars as pl

from pipeforge_worker.contracts.operations import OperationConfigError


class DatasetFormat(StrEnum):
    CSV = "CSV"
    JSONL = "JSONL"
    PARQUET = "PARQUET"


class BadRowPolicy(StrEnum):
    FAIL_FAST = "FAIL_FAST"
    SKIP_AND_REPORT = "SKIP_AND_REPORT"
    QUARANTINE = "QUARANTINE"


@dataclass(frozen=True, slots=True)
class ReaderOptions:
    chunk_size: int = 10_000
    bad_row_policy: BadRowPolicy = BadRowPolicy.FAIL_FAST
    encoding: str | None = None
    delimiter: str | None = None
    has_header: bool | None = None
    max_bytes: int = 512 * 1024 * 1024
    max_rows: int = 10_000_000
    sniff_bytes: int = 64 * 1024
    max_diagnostics: int = 128

    def __post_init__(self) -> None:
        if not 1 <= self.chunk_size <= 1_000_000:
            raise OperationConfigError("chunk_size must be between 1 and 1000000")
        if not isinstance(self.bad_row_policy, BadRowPolicy):
            object.__setattr__(self, "bad_row_policy", BadRowPolicy(self.bad_row_policy))
        if self.delimiter is not None and len(self.delimiter) != 1:
            raise OperationConfigError("delimiter must be one character")
        if not 1 <= self.max_bytes <= 8 * 1024 * 1024 * 1024:
            raise OperationConfigError("max_bytes is outside the supported bound")
        if not 1 <= self.max_rows <= 1_000_000_000:
            raise OperationConfigError("max_rows is outside the supported bound")
        if not 1024 <= self.sniff_bytes <= 1024 * 1024:
            raise OperationConfigError("sniff_bytes must be between 1024 and 1048576")
        if self.sniff_bytes > self.max_bytes:
            raise OperationConfigError("sniff_bytes cannot exceed max_bytes")
        if not 1 <= self.max_diagnostics <= 10_000:
            raise OperationConfigError("max_diagnostics must be between 1 and 10000")


@dataclass(frozen=True, slots=True)
class RowDiagnostic:
    row_number: int
    action: BadRowPolicy
    reason: str
    row_hash: str


@dataclass
class ReaderStats:
    rows_seen: int = 0
    rows_emitted: int = 0
    malformed_rows: int = 0
    skipped_rows: int = 0
    quarantined_rows: int = 0
    bytes_read: int = 0
    diagnostics: list[RowDiagnostic] = field(default_factory=list)

    def snapshot(self) -> ReaderStatsSnapshot:
        return ReaderStatsSnapshot(
            rows_seen=self.rows_seen,
            rows_emitted=self.rows_emitted,
            malformed_rows=self.malformed_rows,
            skipped_rows=self.skipped_rows,
            quarantined_rows=self.quarantined_rows,
            bytes_read=self.bytes_read,
            diagnostics=tuple(self.diagnostics),
        )


@dataclass(frozen=True, slots=True)
class ReaderStatsSnapshot:
    rows_seen: int
    rows_emitted: int
    malformed_rows: int
    skipped_rows: int
    quarantined_rows: int
    bytes_read: int
    diagnostics: tuple[RowDiagnostic, ...]


@dataclass
class ReaderMetadata:
    format: DatasetFormat
    columns: tuple[str, ...] = ()
    encoding: str | None = None
    delimiter: str | None = None
    has_header: bool = False
    estimated_rows: int | None = None


@dataclass(frozen=True, slots=True)
class DataChunk:
    index: int
    row_start: int
    frame: pl.DataFrame


@dataclass
class ReaderSession:
    metadata: ReaderMetadata
    stats: ReaderStats
    _chunks: Iterator[DataChunk]

    def iter_chunks(self) -> Iterator[DataChunk]:
        return self._chunks


class DatasetReader(Protocol):
    def open(
        self,
        source: BinaryIO,
        *,
        source_name: str | None,
        content_type: str | None,
        options: ReaderOptions,
    ) -> AbstractContextManager[ReaderSession]: ...


class ReaderError(ValueError):
    """Base class for non-retryable reader failures."""


class UnsupportedFormatError(ReaderError):
    """Raised when an input format is not supported."""


class FormatConflictError(ReaderError):
    """Raised when explicit format signals disagree."""


class MalformedRowError(ReaderError):
    """Raised for a malformed row under FAIL_FAST policy."""


class DatasetLimitError(ReaderError):
    """Raised when the configured byte or row bound is exceeded."""

"""Protocols and immutable values used by the processing pipeline."""

from __future__ import annotations

import time
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from typing import Protocol
from uuid import UUID

from pipeforge_worker.contracts.operations import OperationRequest, OperationType
from pipeforge_worker.readers.models import DataChunk, ReaderMetadata, ReaderStatsSnapshot


class CancellationRequested(RuntimeError):
    """Raised when a cooperative cancellation request is observed."""


@dataclass(frozen=True, slots=True)
class ProgressSnapshot:
    rows_processed: int
    estimated_rows: int | None
    fraction: float | None
    final: bool = False


@dataclass(frozen=True, slots=True)
class ProcessingContext:
    job_id: UUID
    attempt_id: UUID
    lease_id: UUID
    worker_id: UUID
    is_cancelled: Callable[[], bool] = field(default=lambda: False, compare=False, repr=False)
    on_progress: Callable[[ProgressSnapshot], None] = field(
        default=lambda _snapshot: None,
        compare=False,
        repr=False,
    )
    monotonic: Callable[[], float] = field(default=time.monotonic, compare=False, repr=False)

    def check_cancellation(self) -> None:
        if self.is_cancelled():
            raise CancellationRequested("processing cancellation requested")


@dataclass(frozen=True, slots=True)
class ProcessingResult:
    metadata: ReaderMetadata
    stats: ReaderStatsSnapshot
    operation_results: Mapping[str, Mapping[str, object]]


class ChunkAggregator(Protocol):
    def start(self, metadata: ReaderMetadata) -> None: ...

    def consume(self, chunk: DataChunk) -> None: ...

    def finish(self) -> Mapping[str, object]: ...


AggregatorBuilder = Callable[[OperationRequest], ChunkAggregator]


class OperationDispatcher(Protocol):
    def build(self, operation: OperationRequest) -> ChunkAggregator: ...


class MappingOperationDispatcher:
    """Dispatch operation types without coupling the pipeline to a data operation."""

    def __init__(self, builders: Mapping[OperationType, AggregatorBuilder]) -> None:
        self._builders = dict(builders)

    def build(self, operation: OperationRequest) -> ChunkAggregator:
        try:
            builder = self._builders[operation.type]
        except KeyError as exc:
            raise ValueError(f"no processor registered for {operation.type.value}") from exc
        return builder(operation)


class ProgressReporter:
    def __init__(self, context: ProcessingContext, min_interval_seconds: float = 1.0) -> None:
        if not 0.01 <= min_interval_seconds <= 3600:
            raise ValueError("progress interval is outside the supported bound")
        self._context = context
        self._min_interval_seconds = min_interval_seconds
        self._last_emit_at: float | None = None
        self._last_rows = -1

    def emit(self, rows_processed: int, estimated_rows: int | None, final: bool = False) -> None:
        now = self._context.monotonic()
        if not final and self._last_emit_at is not None:
            if now - self._last_emit_at < self._min_interval_seconds:
                return
            if rows_processed == self._last_rows:
                return
        fraction = None
        if estimated_rows is not None and estimated_rows > 0:
            fraction = min(1.0, rows_processed / estimated_rows)
        self._context.on_progress(
            ProgressSnapshot(
                rows_processed=rows_processed,
                estimated_rows=estimated_rows,
                fraction=fraction,
                final=final,
            )
        )
        self._last_emit_at = now
        self._last_rows = rows_processed

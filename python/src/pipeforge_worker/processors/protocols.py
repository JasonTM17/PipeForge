"""Protocols and immutable values used by the processing pipeline."""

from __future__ import annotations

import time
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
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
    stage: str = "PROCESS"
    throughput: float = 0.0
    updated_at: datetime = field(default_factory=lambda: datetime.now(UTC))


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
    utc_now: Callable[[], datetime] = field(
        default=lambda: datetime.now(UTC), compare=False, repr=False
    )

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
    def __init__(
        self,
        context: ProcessingContext,
        min_interval_seconds: float = 1.0,
        min_progress_delta_percent: float = 1.0,
    ) -> None:
        if not 0.01 <= min_interval_seconds <= 3600:
            raise ValueError("progress interval is outside the supported bound")
        if not 0.01 <= min_progress_delta_percent <= 100.0:
            raise ValueError("progress delta is outside the supported bound")
        self._context = context
        self._min_interval_seconds = min_interval_seconds
        self._min_progress_delta_percent = min_progress_delta_percent
        self._last_emit_at: float | None = None
        self._last_rows = -1
        self._last_fraction: float | None = None
        self._last_stage: str | None = None
        self._started_at: float | None = None

    def emit(
        self,
        rows_processed: int,
        estimated_rows: int | None,
        final: bool = False,
        stage: str = "PROCESS",
    ) -> None:
        if rows_processed < 0:
            raise ValueError("rows_processed must not be negative")
        if estimated_rows is not None and estimated_rows < 0:
            raise ValueError("estimated_rows must not be negative")
        if estimated_rows is not None and estimated_rows < rows_processed:
            raise ValueError("estimated_rows must not be less than rows_processed")
        allowed_stage_characters = (
            "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
        )
        if (
            not stage
            or len(stage) > 64
            or any(char not in allowed_stage_characters for char in stage)
        ):
            raise ValueError("stage contains unsupported characters")
        now = self._context.monotonic()
        if self._started_at is None:
            self._started_at = now
        fraction = None
        if estimated_rows is not None and estimated_rows > 0:
            fraction = min(1.0, rows_processed / estimated_rows)
        if not final and self._last_emit_at is not None:
            elapsed_due = now - self._last_emit_at >= self._min_interval_seconds
            percent_due = False
            if fraction is not None and self._last_fraction is not None:
                percent_due = (
                    abs(fraction - self._last_fraction) * 100 >= self._min_progress_delta_percent
                )
            stage_due = stage != self._last_stage
            same_rows_without_stage_change = rows_processed == self._last_rows and not stage_due
            no_threshold_reached = not elapsed_due and not percent_due and not stage_due
            if same_rows_without_stage_change or no_threshold_reached:
                return
        elapsed = max(0.0, now - self._started_at)
        throughput = rows_processed / elapsed if elapsed > 0 else 0.0
        self._context.on_progress(
            ProgressSnapshot(
                rows_processed=rows_processed,
                estimated_rows=estimated_rows,
                fraction=fraction,
                final=final,
                stage=stage,
                throughput=throughput,
                updated_at=self._context.utc_now().astimezone(UTC),
            )
        )
        self._last_emit_at = now
        self._last_rows = rows_processed
        self._last_fraction = fraction
        self._last_stage = stage

"""Chunk aggregator for deterministic, bounded dataset profiles."""

from __future__ import annotations

import hashlib
import time
from collections.abc import Callable
from typing import Any

from pipeforge_worker.profilers.column import ColumnProfiler
from pipeforge_worker.profilers.config import ProfileConfig
from pipeforge_worker.readers.models import DataChunk, ReaderMetadata, ReaderStatsSnapshot


class ProfileAggregator:
    def __init__(
        self,
        config: ProfileConfig,
        *,
        monotonic: Callable[[], float] = time.monotonic,
    ) -> None:
        self._config = config
        self._monotonic = monotonic
        self._started_at = 0.0
        self._metadata: ReaderMetadata | None = None
        self._columns: dict[str, ColumnProfiler] = {}
        self._rows = 0
        self._chunks = 0
        self._missing_cells = 0
        self._memory_peak = 0
        self._duplicate_rows = 0
        self._duplicate_keys: set[str] = set()
        self._duplicate_capped = False
        self._warnings: list[str] = []

    def start(self, metadata: ReaderMetadata) -> None:
        self._started_at = self._monotonic()
        self._metadata = metadata
        for name in metadata.columns:
            self._columns[name] = ColumnProfiler(name, self._config)

    def consume(self, chunk: DataChunk) -> None:
        if self._metadata is None:
            raise RuntimeError("profile aggregator was not started")
        previous_rows = self._rows
        for name in chunk.frame.columns:
            if name not in self._columns:
                profiler = ColumnProfiler(name, self._config)
                profiler.add_missing(previous_rows)
                self._columns[name] = profiler
        for name, profiler in self._columns.items():
            if name in chunk.frame.columns:
                profiler.consume(chunk.frame.get_column(name).to_list())
            else:
                profiler.add_missing(chunk.frame.height)
        for row in chunk.frame.iter_rows():
            self._observe_duplicate(row)
        self._rows += chunk.frame.height
        self._chunks += 1
        self._missing_cells = sum(profiler.missing_count for profiler in self._columns.values())
        self._memory_peak = max(self._memory_peak, int(chunk.frame.estimated_size()))

    def finish(self) -> dict[str, object]:
        return self.finish_with_stats(ReaderStatsSnapshot(0, self._rows, 0, 0, 0, 0, ()))

    def finish_with_stats(self, stats: ReaderStatsSnapshot) -> dict[str, object]:
        if self._metadata is None:
            raise RuntimeError("profile aggregator was not started")
        duration = max(0.0, self._monotonic() - self._started_at)
        warnings = list(self._warnings)
        for profiler in self._columns.values():
            warnings.extend(profiler.warnings)
        warnings.extend(item.reason for item in stats.diagnostics)
        if self._duplicate_capped:
            warnings.append("duplicate row count unavailable after the distinct limit")
        if self._memory_peak > self._config.max_memory_bytes:
            warnings.append("estimated chunk memory exceeded the configured profile budget")
        duplicate_count: int | None = None if self._duplicate_capped else self._duplicate_rows
        return {
            "schemaVersion": 1,
            "dataset": {
                "fileSizeBytes": stats.bytes_read,
                "rowCount": self._rows,
                "columnCount": len(self._columns),
                "duplicateRowCount": duplicate_count,
                "missingCellCount": self._missing_cells,
                "estimatedMemoryBytes": self._memory_peak,
                "processingDurationSeconds": round(duration, 6),
                "throughputRowsPerSecond": round(self._rows / duration, 6) if duration else 0.0,
                "chunkCount": self._chunks,
                "parsingWarningCount": len(stats.diagnostics),
            },
            "columns": [self._columns[name].finish() for name in sorted(self._columns)],
            "warnings": sorted(set(warnings)),
        }

    def _observe_duplicate(self, row: tuple[Any, ...]) -> None:
        if self._duplicate_capped:
            return
        key = hashlib.sha256(repr(row).encode("utf-8", "replace")).hexdigest()
        if key in self._duplicate_keys:
            self._duplicate_rows += 1
            return
        self._duplicate_keys.add(key)
        if len(self._duplicate_keys) > self._config.max_distinct_values:
            self._duplicate_capped = True
            self._duplicate_keys.clear()

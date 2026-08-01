"""Streaming JSON Lines reader with bounded row diagnostics."""

from __future__ import annotations

import io
import json
from collections.abc import Iterator
from contextlib import AbstractContextManager, contextmanager
from typing import Any, BinaryIO

import polars as pl

from pipeforge_worker.readers.common import add_bad_row, detect_encoding
from pipeforge_worker.readers.detection import detect_format
from pipeforge_worker.readers.models import (
    DataChunk,
    DatasetFormat,
    DatasetLimitError,
    ReaderMetadata,
    ReaderOptions,
    ReaderSession,
    ReaderStats,
    UnsupportedFormatError,
)
from pipeforge_worker.readers.stream import limited_stream, prepare_stream


class JsonlReader:
    def open(
        self,
        source: BinaryIO,
        *,
        source_name: str | None,
        content_type: str | None,
        options: ReaderOptions,
    ) -> AbstractContextManager[ReaderSession]:
        return self._open(
            source,
            source_name=source_name,
            content_type=content_type,
            options=options,
        )

    @contextmanager
    def _open(
        self,
        source: BinaryIO,
        *,
        source_name: str | None,
        content_type: str | None,
        options: ReaderOptions,
    ) -> Iterator[ReaderSession]:
        with prepare_stream(source, options.sniff_bytes) as prepared:
            detected = detect_format(
                prefix=prepared.prefix, source_name=source_name, content_type=content_type
            )
            if detected is not DatasetFormat.JSONL:
                raise UnsupportedFormatError(f"JSONL reader cannot read {detected.value}")
            encoding = detect_encoding(prepared.prefix, options.encoding)
            metadata = ReaderMetadata(format=DatasetFormat.JSONL, encoding=encoding)
            stats = ReaderStats(bytes_read=len(prepared.prefix))
            yield ReaderSession(
                metadata=metadata,
                stats=stats,
                _chunks=self._iter_chunks(prepared.stream, metadata, stats, options),
            )

    def _iter_chunks(
        self,
        source: BinaryIO,
        metadata: ReaderMetadata,
        stats: ReaderStats,
        options: ReaderOptions,
    ) -> Iterator[DataChunk]:
        with limited_stream(source, options.max_bytes) as (bounded, limiter):
            text = io.TextIOWrapper(bounded, encoding=metadata.encoding or "utf-8", newline="")
            try:
                columns: list[str] = []
                records: list[dict[str, Any]] = []
                chunk_start = 0
                chunk_index = 0
                for line_number, raw_line in enumerate(text, start=1):
                    line = raw_line.strip()
                    if not line:
                        continue
                    stats.rows_seen += 1
                    if stats.rows_seen > options.max_rows:
                        raise DatasetLimitError("dataset exceeds the configured row limit")
                    try:
                        value = json.loads(line)
                        if not isinstance(value, dict):
                            raise ValueError("JSONL row must be an object")
                        record = {str(key): item for key, item in value.items()}
                    except (json.JSONDecodeError, TypeError, ValueError) as exc:
                        add_bad_row(
                            stats,
                            options.bad_row_policy,
                            line_number,
                            str(exc),
                            line,
                            options.max_diagnostics,
                        )
                        continue
                    for key in record:
                        if key not in columns:
                            columns.append(key)
                    metadata.columns = tuple(columns)
                    if not records:
                        chunk_start = line_number
                    records.append(record)
                    if len(records) >= options.chunk_size:
                        yield _make_chunk(chunk_index, chunk_start, records, columns, stats)
                        chunk_index += 1
                        records = []
                if records:
                    yield _make_chunk(chunk_index, chunk_start, records, columns, stats)
            finally:
                stats.bytes_read = limiter.count
                text.detach()


def _make_chunk(
    index: int,
    row_start: int,
    records: list[dict[str, Any]],
    columns: list[str],
    stats: ReaderStats,
) -> DataChunk:
    frame = pl.from_dicts(records, schema=columns, strict=False)
    stats.rows_emitted += frame.height
    return DataChunk(index=index, row_start=row_start, frame=frame)

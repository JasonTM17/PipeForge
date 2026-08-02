"""Bounded Parquet batch reader backed by PyArrow and Polars frames."""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import AbstractContextManager, contextmanager
from typing import BinaryIO, cast

import polars as pl
import pyarrow as pa
import pyarrow.parquet as pq

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
from pipeforge_worker.readers.stream import prepare_stream, seekable_copy


class ParquetReader:
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
            if detected is not DatasetFormat.PARQUET:
                raise UnsupportedFormatError(f"Parquet reader cannot read {detected.value}")
            with seekable_copy(
                prepared.stream,
                max_bytes=options.max_bytes,
                memory_limit=min(options.max_bytes, max(options.chunk_size * 64, 1024 * 1024)),
            ) as seekable:
                parquet = pq.ParquetFile(seekable)
                row_count = parquet.metadata.num_rows if parquet.metadata is not None else None
                if row_count is not None and row_count > options.max_rows:
                    raise DatasetLimitError("dataset exceeds the configured row limit")
                columns = tuple(str(name) for name in parquet.schema_arrow.names)
                metadata = ReaderMetadata(
                    format=DatasetFormat.PARQUET,
                    columns=columns,
                    has_header=False,
                    estimated_rows=row_count,
                )
                stats = ReaderStats(bytes_read=len(prepared.prefix))
                yield ReaderSession(
                    metadata=metadata,
                    stats=stats,
                    _chunks=self._iter_chunks(parquet, columns, stats, options),
                )

    def _iter_chunks(
        self,
        parquet: pq.ParquetFile,
        columns: tuple[str, ...],
        stats: ReaderStats,
        options: ReaderOptions,
    ) -> Iterator[DataChunk]:
        for index, batch in enumerate(parquet.iter_batches(batch_size=options.chunk_size)):
            frame = cast(pl.DataFrame, pl.from_arrow(cast(pa.RecordBatch, batch)))
            if stats.rows_emitted + frame.height > options.max_rows:
                raise DatasetLimitError("dataset exceeds the configured row limit")
            row_start = stats.rows_emitted + 1
            stats.rows_seen += frame.height
            stats.rows_emitted += frame.height
            if tuple(frame.columns) != columns:
                frame = frame.select(list(columns))
            yield DataChunk(index=index, row_start=row_start, frame=frame)

"""Streaming CSV reader with bounded diagnostics and malformed-row policies."""

from __future__ import annotations

import csv
import io
from collections.abc import Iterator
from contextlib import AbstractContextManager, contextmanager
from typing import BinaryIO

import polars as pl

from pipeforge_worker.readers.common import add_bad_row, detect_encoding, normalize_columns
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
from pipeforge_worker.readers.stream import PreparedStream, limited_stream, prepare_stream


class CsvReader:
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
            with self._open_prepared(
                prepared, source_name=source_name, content_type=content_type, options=options
            ) as session:
                yield session

    @contextmanager
    def _open_prepared(
        self,
        prepared: PreparedStream,
        *,
        source_name: str | None,
        content_type: str | None,
        options: ReaderOptions,
    ) -> Iterator[ReaderSession]:
        detected = detect_format(
            prefix=prepared.prefix, source_name=source_name, content_type=content_type
        )
        if detected is not DatasetFormat.CSV:
            raise UnsupportedFormatError(f"CSV reader cannot read {detected.value}")
        encoding = detect_encoding(prepared.prefix, options.encoding)
        sample = prepared.prefix.decode(encoding, errors="replace")
        delimiter = options.delimiter or _detect_delimiter(sample)
        has_header = (
            options.has_header if options.has_header is not None else _detect_header(sample)
        )
        metadata = ReaderMetadata(
            format=DatasetFormat.CSV,
            encoding=encoding,
            delimiter=delimiter,
            has_header=has_header,
        )
        stats = ReaderStats(bytes_read=len(prepared.prefix))
        session = ReaderSession(
            metadata=metadata,
            stats=stats,
            _chunks=self._iter_chunks(prepared.stream, metadata, stats, options),
        )
        yield session

    def _iter_chunks(
        self,
        source: BinaryIO,
        metadata: ReaderMetadata,
        stats: ReaderStats,
        options: ReaderOptions,
    ) -> Iterator[DataChunk]:
        with limited_stream(source, options.max_bytes) as (bounded, limiter):
            text = io.TextIOWrapper(
                bounded,
                encoding=metadata.encoding or "utf-8",
                errors="strict",
                newline="",
            )
            try:
                reader = csv.reader(
                    text,
                    delimiter=metadata.delimiter or ",",
                    quotechar='"',
                    strict=True,
                )
                columns: tuple[str, ...] = ()
                chunk_rows: list[list[str]] = []
                chunk_start = 0
                chunk_index = 0
                first_row = True
                while True:
                    try:
                        row = next(reader)
                    except StopIteration:
                        break
                    except csv.Error as exc:
                        add_bad_row(
                            stats,
                            options.bad_row_policy,
                            max(reader.line_num, 1),
                            "invalid CSV quoting",
                            str(exc),
                            options.max_diagnostics,
                        )
                        continue
                    if first_row and metadata.has_header:
                        columns = normalize_columns(row)
                        metadata.columns = columns
                        first_row = False
                        continue
                    if first_row:
                        columns = normalize_columns(
                            f"column_{index}" for index in range(1, len(row) + 1)
                        )
                        metadata.columns = columns
                        first_row = False
                    stats.rows_seen += 1
                    if stats.rows_seen > options.max_rows:
                        raise DatasetLimitError("dataset exceeds the configured row limit")
                    if len(row) != len(columns):
                        add_bad_row(
                            stats,
                            options.bad_row_policy,
                            max(reader.line_num, 1),
                            f"expected {len(columns)} fields, received {len(row)}",
                            "|".join(row),
                            options.max_diagnostics,
                        )
                        continue
                    if not chunk_rows:
                        chunk_start = max(reader.line_num, 1)
                    chunk_rows.append(row)
                    if len(chunk_rows) >= options.chunk_size:
                        yield _make_chunk(chunk_index, chunk_start, chunk_rows, columns, stats)
                        chunk_index += 1
                        chunk_rows = []
                if chunk_rows:
                    yield _make_chunk(chunk_index, chunk_start, chunk_rows, columns, stats)
            finally:
                stats.bytes_read = limiter.count
                text.detach()


def _make_chunk(
    index: int,
    row_start: int,
    rows: list[list[str]],
    columns: tuple[str, ...],
    stats: ReaderStats,
) -> DataChunk:
    frame = pl.DataFrame(rows, schema=list(columns), orient="row", strict=False)
    stats.rows_emitted += frame.height
    return DataChunk(index=index, row_start=row_start, frame=frame)


def _detect_delimiter(sample: str) -> str:
    try:
        dialect = csv.Sniffer().sniff(sample, delimiters=",;\t|")
    except csv.Error:
        return ","
    return dialect.delimiter


def _detect_header(sample: str) -> bool:
    try:
        return csv.Sniffer().has_header(sample)
    except csv.Error:
        return True

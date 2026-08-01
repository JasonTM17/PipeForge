"""Format-aware reader factory that preserves the initial sniff bytes."""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from typing import BinaryIO

from pipeforge_worker.readers.csv_reader import CsvReader
from pipeforge_worker.readers.detection import detect_format
from pipeforge_worker.readers.jsonl_reader import JsonlReader
from pipeforge_worker.readers.models import (
    DatasetFormat,
    DatasetReader,
    ReaderOptions,
    ReaderSession,
)
from pipeforge_worker.readers.parquet_reader import ParquetReader
from pipeforge_worker.readers.stream import prepare_stream


class DatasetReaderFactory:
    def __init__(self) -> None:
        self._readers: dict[DatasetFormat, DatasetReader] = {
            DatasetFormat.CSV: CsvReader(),
            DatasetFormat.JSONL: JsonlReader(),
            DatasetFormat.PARQUET: ParquetReader(),
        }

    @contextmanager
    def open(
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
            reader = self._readers[detected]
            open_prepared = getattr(reader, "_open_prepared", None)
            if open_prepared is not None:
                with open_prepared(
                    prepared,
                    source_name=source_name,
                    content_type=content_type,
                    options=options,
                ) as session:
                    yield session
                return
            with reader.open(
                prepared.stream,
                source_name=source_name,
                content_type=content_type,
                options=options,
            ) as session:
                yield session

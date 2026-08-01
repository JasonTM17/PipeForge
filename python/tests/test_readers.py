from __future__ import annotations

import io

import polars as pl
import pyarrow as pa  # type: ignore[import-untyped]
import pyarrow.parquet as pq  # type: ignore[import-untyped]
import pytest

from pipeforge_worker.readers.csv_reader import CsvReader
from pipeforge_worker.readers.detection import detect_format
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.readers.models import (
    BadRowPolicy,
    DatasetFormat,
    DatasetLimitError,
    FormatConflictError,
    MalformedRowError,
    ReaderOptions,
)
from pipeforge_worker.readers.parquet_reader import ParquetReader


class NonSeekableBytes(io.BytesIO):
    def seekable(self) -> bool:
        return False

    def seek(self, *_args: object, **_kwargs: object) -> int:
        raise io.UnsupportedOperation("not seekable")

    def tell(self) -> int:
        raise io.UnsupportedOperation("not seekable")


def collect(reader: object, data: bytes, **kwargs: object) -> tuple[object, list[pl.DataFrame]]:
    options = ReaderOptions(**kwargs)
    with reader.open(  # type: ignore[attr-defined]
        io.BytesIO(data), source_name="fixture.csv", content_type="text/csv", options=options
    ) as session:
        chunks = [chunk.frame for chunk in session.iter_chunks()]
        return session, chunks


def test_format_detection_rejects_conflicting_explicit_signals() -> None:
    with pytest.raises(FormatConflictError):
        detect_format(
            prefix=b"PAR1\x00",
            source_name="dataset.csv",
            content_type="text/csv",
        )


def test_csv_detects_delimiter_header_quotes_and_chunks() -> None:
    data = b'name;comment\nAlice;"hello;world"\nBob;ok\n'
    session, chunks = collect(CsvReader(), data, chunk_size=1, has_header=True, delimiter=None)

    assert session.metadata.format is DatasetFormat.CSV
    assert session.metadata.columns == ("name", "comment")
    assert session.metadata.delimiter == ";"
    assert len(chunks) == 2
    assert chunks[0].to_dicts() == [{"name": "Alice", "comment": "hello;world"}]
    assert session.stats.rows_emitted == 2


def test_csv_supports_latin1_and_reports_malformed_rows() -> None:
    data = "name,city\nAndré,Paris\nmissing\nBea,Lyon\n".encode("latin-1")
    session, chunks = collect(
        CsvReader(),
        data,
        encoding=None,
        has_header=True,
        bad_row_policy=BadRowPolicy.SKIP_AND_REPORT,
        chunk_size=10,
    )

    assert session.metadata.encoding == "latin-1"
    assert [row for frame in chunks for row in frame.to_dicts()] == [
        {"name": "André", "city": "Paris"},
        {"name": "Bea", "city": "Lyon"},
    ]
    assert session.stats.malformed_rows == 1
    assert session.stats.skipped_rows == 1
    assert len(session.stats.diagnostics) == 1
    assert "missing" not in session.stats.diagnostics[0].row_hash


def test_csv_fail_fast_rejects_ragged_rows() -> None:
    with pytest.raises(MalformedRowError):
        collect(CsvReader(), b"a,b\n1\n", has_header=True)


def test_jsonl_handles_mixed_columns_and_truncated_line() -> None:
    data = b'{"id":1,"name":"a"}\n{"id":2,"active":true}\n{"id":\n'
    factory = DatasetReaderFactory()
    with factory.open(
        io.BytesIO(data),
        source_name="events.jsonl",
        content_type="application/x-ndjson",
        options=ReaderOptions(chunk_size=2, bad_row_policy=BadRowPolicy.QUARANTINE),
    ) as session:
        chunks = list(session.iter_chunks())

    assert session.metadata.format is DatasetFormat.JSONL
    assert session.metadata.columns == ("id", "name", "active")
    assert chunks[0].frame.to_dicts() == [
        {"id": 1, "name": "a", "active": None},
        {"id": 2, "name": None, "active": True},
    ]
    assert session.stats.quarantined_rows == 1
    assert session.stats.diagnostics[0].row_number == 3


def test_empty_csv_is_a_valid_empty_session() -> None:
    session, chunks = collect(CsvReader(), b"", has_header=True)
    assert chunks == []
    assert session.metadata.columns == ()
    assert session.stats.rows_emitted == 0


def test_parquet_reads_arrow_batches_as_polars_frames() -> None:
    output = io.BytesIO()
    pq.write_table(pa.table({"id": [1, 2, 3], "value": [1.5, 2.5, 3.5]}), output)
    source = NonSeekableBytes(output.getvalue())

    with ParquetReader().open(
        source,
        source_name="values.parquet",
        content_type="application/octet-stream",
        options=ReaderOptions(chunk_size=2),
    ) as session:
        chunks = list(session.iter_chunks())

    assert session.metadata.format is DatasetFormat.PARQUET
    assert session.metadata.columns == ("id", "value")
    assert [chunk.frame.height for chunk in chunks] == [2, 1]
    assert session.stats.rows_emitted == 3


def test_large_csv_is_iterated_in_bounded_chunks() -> None:
    lines = ["id,value"] + [f"{index},{index * 2}" for index in range(20_000)]
    source = NonSeekableBytes(("\n".join(lines) + "\n").encode())
    with DatasetReaderFactory().open(
        source,
        source_name="large.csv",
        content_type="text/csv",
        options=ReaderOptions(chunk_size=500, has_header=True),
    ) as session:
        chunks = list(session.iter_chunks())

    assert len(chunks) == 40
    assert all(chunk.frame.height <= 500 for chunk in chunks)
    assert session.stats.rows_emitted == 20_000


def test_text_reader_enforces_byte_limit() -> None:
    with pytest.raises(DatasetLimitError):
        collect(
            CsvReader(),
            b"id,value\n" + (b"1,2\n" * 400),
            has_header=True,
            max_bytes=1024,
            sniff_bytes=1024,
        )

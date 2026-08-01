"""Deterministic format detection with explicit conflict rejection."""

from __future__ import annotations

import json
from collections.abc import Mapping
from pathlib import PurePath

from pipeforge_worker.readers.models import (
    DatasetFormat,
    FormatConflictError,
    UnsupportedFormatError,
)


def detect_format(
    *,
    prefix: bytes,
    source_name: str | None,
    content_type: str | None,
) -> DatasetFormat:
    signals: list[tuple[str, DatasetFormat]] = []
    magic = _magic_format(prefix)
    if magic is not None:
        signals.append(("magic-bytes", magic))

    content_signal = _content_type_format(content_type)
    if content_signal is not None:
        signals.append(("content-type", content_signal))

    extension_signal = _extension_format(source_name)
    if extension_signal is not None:
        signals.append(("extension", extension_signal))

    distinct = {format_value for _, format_value in signals}
    if len(distinct) > 1:
        details = ", ".join(f"{source}={value}" for source, value in signals)
        raise FormatConflictError(f"input format signals conflict: {details}")
    if signals:
        return signals[0][1]

    sniffed = _sniff_text(prefix)
    if sniffed is not None:
        return sniffed
    raise UnsupportedFormatError("unable to detect a supported dataset format")


def _magic_format(prefix: bytes) -> DatasetFormat | None:
    return DatasetFormat.PARQUET if prefix.startswith(b"PAR1") else None


def _content_type_format(content_type: str | None) -> DatasetFormat | None:
    if not content_type:
        return None
    normalized = content_type.split(";", 1)[0].strip().lower()
    if normalized in {"application/vnd.apache.parquet", "application/x-parquet"}:
        return DatasetFormat.PARQUET
    if normalized in {"application/jsonl", "application/x-ndjson", "application/ndjson"}:
        return DatasetFormat.JSONL
    if normalized in {"text/csv", "application/csv", "application/vnd.ms-excel"}:
        return DatasetFormat.CSV
    return None


def _extension_format(source_name: str | None) -> DatasetFormat | None:
    if not source_name:
        return None
    suffix = PurePath(source_name.split("?", 1)[0]).suffix.lower()
    if suffix == ".parquet":
        return DatasetFormat.PARQUET
    if suffix in {".jsonl", ".ndjson", ".json"}:
        return DatasetFormat.JSONL
    if suffix in {".csv", ".tsv", ".txt"}:
        return DatasetFormat.CSV
    return None


def _sniff_text(prefix: bytes) -> DatasetFormat | None:
    try:
        text = prefix.decode("utf-8-sig")
    except UnicodeDecodeError:
        return None
    first_line = next((line.strip() for line in text.splitlines() if line.strip()), "")
    if not first_line:
        return None
    try:
        value = json.loads(first_line)
    except json.JSONDecodeError:
        return DatasetFormat.CSV
    if isinstance(value, Mapping):
        return DatasetFormat.JSONL
    raise UnsupportedFormatError("JSON arrays are not supported; use JSON Lines")

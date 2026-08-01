"""Shared diagnostics, encoding, and column handling for text readers."""

from __future__ import annotations

import hashlib
from collections.abc import Iterable

from pipeforge_worker.readers.models import (
    BadRowPolicy,
    MalformedRowError,
    ReaderStats,
    RowDiagnostic,
)


def detect_encoding(sample: bytes, requested: str | None) -> str:
    if requested:
        _validate_encoding(requested)
        return requested
    if sample.startswith(b"\xef\xbb\xbf"):
        return "utf-8-sig"
    if sample.startswith((b"\xff\xfe", b"\xfe\xff")):
        return "utf-16"
    try:
        sample.decode("utf-8")
    except UnicodeDecodeError:
        return "latin-1"
    return "utf-8"


def normalize_columns(values: Iterable[str]) -> tuple[str, ...]:
    result: list[str] = []
    counts: dict[str, int] = {}
    for index, raw in enumerate(values, start=1):
        base = raw.strip() or f"column_{index}"
        count = counts.get(base, 0) + 1
        counts[base] = count
        result.append(base if count == 1 else f"{base}_{count}")
    return tuple(result)


def add_bad_row(
    stats: ReaderStats,
    policy: BadRowPolicy,
    row_number: int,
    reason: str,
    raw_value: str,
    max_diagnostics: int,
) -> None:
    stats.malformed_rows += 1
    if policy is BadRowPolicy.FAIL_FAST:
        raise MalformedRowError(f"malformed row {row_number}: {reason}")
    stats.skipped_rows += 1
    if policy is BadRowPolicy.QUARANTINE:
        stats.quarantined_rows += 1
    if len(stats.diagnostics) < max_diagnostics:
        digest = hashlib.sha256(raw_value[:4096].encode("utf-8", "replace")).hexdigest()[:16]
        stats.diagnostics.append(
            RowDiagnostic(
                row_number=row_number,
                action=policy,
                reason=reason[:128],
                row_hash=digest,
            )
        )


def _validate_encoding(value: str) -> None:
    try:
        "".encode(value)
    except LookupError as exc:
        raise ValueError(f"unsupported text encoding: {value}") from exc

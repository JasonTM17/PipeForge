"""Non-sensitive bounded references emitted by anomaly reports."""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class AnomalyObservation:
    row_number: int
    value: float
    row_hash: str


def anomaly_reference(observation: AnomalyObservation) -> dict[str, object]:
    return {"rowNumber": observation.row_number, "rowHash": observation.row_hash}


def row_hash(column: str, value: object) -> str:
    encoded = json.dumps(
        {column: json_safe(value)}, sort_keys=True, separators=(",", ":"), default=str
    ).encode()
    return hashlib.sha256(encoded).hexdigest()[:16]


def json_safe(value: object) -> object:
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    isoformat = getattr(value, "isoformat", None)
    return str(isoformat()) if callable(isoformat) else str(value)

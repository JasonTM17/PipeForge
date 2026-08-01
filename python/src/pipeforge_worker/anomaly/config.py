"""Strict configuration for bounded anomaly detection."""

from __future__ import annotations

import math
import re
from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum

from pipeforge_worker.contracts.operations import OperationConfigError

_COLUMN_PATTERN = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{0,127}$")
_DEFAULT_THRESHOLDS = {
    "IQR": 1.5,
    "Z_SCORE": 3.0,
    "MODIFIED_Z_SCORE": 3.5,
}


class AnomalyMethod(StrEnum):
    IQR = "IQR"
    Z_SCORE = "Z_SCORE"
    MODIFIED_Z_SCORE = "MODIFIED_Z_SCORE"


class NullPolicy(StrEnum):
    SKIP = "SKIP"
    FAIL = "FAIL"


@dataclass(frozen=True, slots=True)
class AnomalyConfig:
    column: str
    method: AnomalyMethod
    threshold: float
    null_policy: NullPolicy
    minimum_sample_size: int
    sample_output_limit: int

    @classmethod
    def from_mapping(cls, value: Mapping[str, object]) -> AnomalyConfig:
        allowed = {
            "column",
            "method",
            "threshold",
            "nullPolicy",
            "minimumSampleSize",
            "sampleOutputLimit",
        }
        unknown = set(value) - allowed
        if unknown:
            raise OperationConfigError(f"anomaly config has unknown fields: {sorted(unknown)}")

        column = value.get("column")
        if not isinstance(column, str) or not _COLUMN_PATTERN.fullmatch(column):
            raise OperationConfigError("anomaly column is invalid")
        try:
            method = AnomalyMethod(str(value.get("method")))
        except ValueError as exc:
            raise OperationConfigError("anomaly method is invalid") from exc

        threshold_value = value.get("threshold", _DEFAULT_THRESHOLDS[method.value])
        threshold = _finite_number(threshold_value, "threshold")
        if not 0.0001 <= threshold <= 1_000:
            raise OperationConfigError("anomaly threshold is outside the supported bound")

        null_policy_value = value.get("nullPolicy", NullPolicy.SKIP.value)
        try:
            null_policy = NullPolicy(str(null_policy_value))
        except ValueError as exc:
            raise OperationConfigError("anomaly nullPolicy is invalid") from exc

        minimum_sample_size = _bounded_int(
            value.get("minimumSampleSize", 3), "minimumSampleSize", 3, 1_000_000
        )
        sample_output_limit = _bounded_int(
            value.get("sampleOutputLimit", 128), "sampleOutputLimit", 0, 128
        )
        return cls(
            column=column,
            method=method,
            threshold=threshold,
            null_policy=null_policy,
            minimum_sample_size=minimum_sample_size,
            sample_output_limit=sample_output_limit,
        )


def _finite_number(value: object, name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise OperationConfigError(f"anomaly {name} must be a finite number")
    number = float(value)
    if not math.isfinite(number):
        raise OperationConfigError(f"anomaly {name} must be a finite number")
    return number


def _bounded_int(value: object, name: str, minimum: int, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise OperationConfigError(f"anomaly {name} is outside the supported bound")
    return value

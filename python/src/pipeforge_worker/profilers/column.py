"""Bounded per-column profile state."""

from __future__ import annotations

import math
from collections import Counter
from collections.abc import Iterable
from datetime import date, datetime
from typing import Any

from pipeforge_worker.profilers.bounded import BoundedFrequencies, DistinctCounter, ReservoirSampler
from pipeforge_worker.profilers.config import ProfileConfig


class ColumnProfiler:
    def __init__(self, name: str, config: ProfileConfig) -> None:
        self.name = name
        self._config = config
        self.total_rows = 0
        self.missing_count = 0
        self._types: Counter[str] = Counter()
        self._distinct = DistinctCounter(config.max_distinct_values, config.distinct_strategy)
        seed = config.sample_seed + _stable_seed(name)
        self._numeric_sample = ReservoirSampler(config.sample_size, seed)
        self._frequencies = BoundedFrequencies(config.max_common_values)
        self._numeric_count = 0
        self._numeric_sum = 0.0
        self._numeric_sum_squares = 0.0
        self._numeric_min: float | None = None
        self._numeric_max: float | None = None
        self._string_min: int | None = None
        self._string_max: int | None = None

    def add_missing(self, count: int) -> None:
        self.total_rows += count
        self.missing_count += count

    def consume(self, values: Iterable[Any]) -> None:
        for value in values:
            self.total_rows += 1
            if _is_missing(value):
                self.missing_count += 1
                continue
            value_type = _type_name(value)
            self._types[value_type] += 1
            self._distinct.add(value)
            self._frequencies.add(value)
            numeric = _numeric_value(value)
            if numeric is not None:
                self._numeric_count += 1
                self._numeric_sum += numeric
                self._numeric_sum_squares += numeric * numeric
                self._numeric_min = (
                    numeric if self._numeric_min is None else min(self._numeric_min, numeric)
                )
                self._numeric_max = (
                    numeric if self._numeric_max is None else max(self._numeric_max, numeric)
                )
                self._numeric_sample.add(numeric)
            if isinstance(value, str):
                length = len(value)
                self._string_min = (
                    length if self._string_min is None else min(self._string_min, length)
                )
                self._string_max = (
                    length if self._string_max is None else max(self._string_max, length)
                )

    def finish(self) -> dict[str, object]:
        non_null = self.total_rows - self.missing_count
        inferred_type = "unknown"
        dominant_count = 0
        if self._types:
            inferred_type, dominant_count = min(
                self._types.items(), key=lambda item: (-item[1], item[0])
            )
        result: dict[str, object] = {
            "name": self.name,
            "inferredType": inferred_type,
            "nullable": self.missing_count > 0,
            "missingCount": self.missing_count,
            "missingPercentage": _percentage(self.missing_count, self.total_rows),
            "distinctCount": self._distinct.count(),
            "distinctApproximate": self._distinct.approximate,
            "minimum": self._numeric_min,
            "maximum": self._numeric_max,
            "mean": None,
            "median": None,
            "standardDeviation": None,
            "quantiles": {},
            "minimumStringLength": self._string_min,
            "maximumStringLength": self._string_max,
            "commonValues": [],
            "typeViolationCount": max(0, non_null - dominant_count),
        }
        if self._numeric_count:
            mean = self._numeric_sum / self._numeric_count
            variance = max(
                0.0,
                self._numeric_sum_squares / self._numeric_count - mean * mean,
            )
            result["mean"] = _finite(mean)
            result["standardDeviation"] = _finite(math.sqrt(variance))
            result["median"] = _finite(_quantile(self._numeric_sample.values, 0.5))
            result["quantiles"] = {
                _quantile_key(level): _finite(_quantile(self._numeric_sample.values, level))
                for level in self._config.quantiles
            }
        if self._config.include_common_values and self.name not in self._config.sensitive_columns:
            result["commonValues"] = self._frequencies.top()
        return result

    @property
    def warnings(self) -> list[str]:
        warnings: list[str] = []
        if self._distinct.approximate:
            warnings.append(f"{self.name}: distinct count is approximate")
        if self._numeric_sample.sampled:
            warnings.append(f"{self.name}: quantiles use a bounded reservoir sample")
        if self._frequencies.truncated:
            warnings.append(f"{self.name}: common values were bounded")
        if self.total_rows - self.missing_count > 0 and len(self._types) > 1:
            warnings.append(f"{self.name}: mixed inferred types detected")
        return warnings


def _is_missing(value: Any) -> bool:
    return value is None or (isinstance(value, float) and math.isnan(value))


def _type_name(value: Any) -> str:
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, (int, float)):
        return "numeric"
    if isinstance(value, datetime):
        return "datetime"
    if isinstance(value, date):
        return "date"
    if isinstance(value, str):
        return "string"
    return type(value).__name__


def _numeric_value(value: Any) -> float | None:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    converted = float(value)
    return converted if math.isfinite(converted) else None


def _percentage(numerator: int, denominator: int) -> float:
    return round(numerator / denominator * 100, 6) if denominator else 0.0


def _quantile(values: list[float], level: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    position = (len(ordered) - 1) * level
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    weight = position - lower
    return ordered[lower] * (1 - weight) + ordered[upper] * weight


def _quantile_key(level: float) -> str:
    return f"{level:.6f}".rstrip("0").rstrip(".")


def _finite(value: float) -> float:
    return value if math.isfinite(value) else 0.0


def _stable_seed(value: str) -> int:
    return sum((index + 1) * ord(char) for index, char in enumerate(value))

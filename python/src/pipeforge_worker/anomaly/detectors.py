"""Pure reference detectors with explicit finite/zero-variance behavior."""

from __future__ import annotations

import math
from collections.abc import Sequence
from dataclasses import dataclass

from pipeforge_worker.anomaly.config import AnomalyMethod


@dataclass(frozen=True, slots=True)
class Detection:
    indices: tuple[int, ...]
    warning: str | None = None


def detect(
    values: Sequence[float], method: AnomalyMethod, threshold: float, minimum: int
) -> Detection:
    if len(values) < minimum:
        return Detection((), "INSUFFICIENT_SAMPLE")
    if not values or any(not math.isfinite(value) for value in values):
        return Detection((), "NON_FINITE_INPUT")
    if method is AnomalyMethod.IQR:
        lower_quartile = _quantile(values, 0.25)
        upper_quartile = _quantile(values, 0.75)
        spread = upper_quartile - lower_quartile
        if spread == 0:
            return Detection((), "ZERO_VARIANCE")
        lower = lower_quartile - threshold * spread
        upper = upper_quartile + threshold * spread
        return Detection(
            tuple(index for index, value in enumerate(values) if value < lower or value > upper)
        )
    if method is AnomalyMethod.Z_SCORE:
        mean = sum(values) / len(values)
        variance = sum((value - mean) ** 2 for value in values) / len(values)
        spread = math.sqrt(variance)
        if spread == 0:
            return Detection((), "ZERO_VARIANCE")
        return Detection(
            tuple(
                index
                for index, value in enumerate(values)
                if abs(value - mean) / spread > threshold
            )
        )
    median = _quantile(values, 0.5)
    deviations = [abs(value - median) for value in values]
    median_absolute_deviation = _quantile(deviations, 0.5)
    if median_absolute_deviation == 0:
        return Detection((), "ZERO_VARIANCE")
    return Detection(
        tuple(
            index
            for index, value in enumerate(values)
            if 0.6745 * abs(value - median) / median_absolute_deviation > threshold
        )
    )


def _quantile(values: Sequence[float], fraction: float) -> float:
    ordered = sorted(values)
    position = (len(ordered) - 1) * fraction
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    weight = position - lower
    return ordered[lower] + (ordered[upper] - ordered[lower]) * weight

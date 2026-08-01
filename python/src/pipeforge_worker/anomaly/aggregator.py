"""Chunk-aware anomaly aggregation with bounded numeric retention."""

from __future__ import annotations

import math
import time
from collections.abc import Callable, Mapping

from pipeforge_worker.anomaly.config import AnomalyConfig
from pipeforge_worker.anomaly.detectors import detect
from pipeforge_worker.anomaly.references import (
    AnomalyObservation,
    anomaly_reference,
    json_safe,
    row_hash,
)
from pipeforge_worker.readers.models import DataChunk, ReaderMetadata, ReaderStatsSnapshot

MAX_RETAINED_VALUES = 100_000


class AnomalyAggregator:
    """Detect anomalies from a deterministic bounded sample of numeric values."""

    def __init__(
        self,
        config: AnomalyConfig,
        *,
        monotonic: Callable[[], float] | None = None,
        max_retained_values: int = MAX_RETAINED_VALUES,
    ) -> None:
        if not 1 <= max_retained_values <= MAX_RETAINED_VALUES:
            raise ValueError("max_retained_values is outside the supported bound")
        self._config = config
        self._monotonic = monotonic or time.monotonic
        self._max_retained_values = max_retained_values
        self._metadata: ReaderMetadata | None = None
        self._started_at = 0.0
        self._initialized = False
        self._error_code: str | None = None
        self._observations: list[AnomalyObservation] = []
        self._total_numeric = 0
        self._invalid_count = 0
        self._invalid_references: list[AnomalyObservation] = []

    def start(self, metadata: ReaderMetadata) -> None:
        self._metadata = metadata
        self._started_at = self._monotonic()
        self._initialized = False
        self._error_code = None
        self._observations = []
        self._total_numeric = 0
        self._invalid_count = 0
        self._invalid_references = []
        if metadata.columns:
            self._initialize(metadata.columns)

    def _initialize(self, columns: tuple[str, ...]) -> None:
        if self._initialized:
            return
        if self._config.column not in columns:
            self._error_code = "MISSING_COLUMN"
        self._initialized = True

    def consume(self, chunk: DataChunk) -> None:
        if self._metadata is None:
            raise RuntimeError("anomaly aggregator was not started")
        if not self._initialized:
            self._metadata.columns = tuple(chunk.frame.columns)
            self._initialize(self._metadata.columns)
        if self._error_code is not None:
            return
        values = chunk.frame.get_column(self._config.column).to_list()
        for offset, raw in enumerate(values):
            row_number = chunk.row_start + offset + 1
            numeric = _numeric_value(raw)
            if numeric is None:
                self._invalid_count += 1
                if self._config.null_policy.value == "FAIL":
                    self._record_invalid(row_number, raw)
                continue
            self._total_numeric += 1
            self._record_observation(row_number, numeric)

    def finish(self) -> Mapping[str, object]:
        return self._finish(None)

    def finish_with_stats(self, stats: ReaderStatsSnapshot) -> Mapping[str, object]:
        return self._finish(stats)

    def _finish(self, stats: ReaderStatsSnapshot | None) -> Mapping[str, object]:
        if self._metadata is None:
            raise RuntimeError("anomaly aggregator was not started")
        if not self._initialized:
            self._initialize(self._metadata.columns)
        warnings: list[str] = []
        if stats is not None and stats.malformed_rows:
            warnings.append("INPUT_MALFORMED_ROWS")
        if self._invalid_count and self._config.null_policy.value == "SKIP":
            warnings.append("NON_NUMERIC_VALUES_SKIPPED")
        sampled = self._total_numeric > len(self._observations)
        if sampled:
            warnings.append("BOUNDED_NUMERIC_SAMPLE")

        references: list[dict[str, object]] = []
        anomaly_count = 0
        status = "PASSED"
        error_code = self._error_code
        if error_code is not None:
            status = "ERROR"
        elif self._total_numeric < self._config.minimum_sample_size:
            status = "SKIPPED"
            error_code = "INSUFFICIENT_SAMPLE"
        else:
            detection = detect(
                [observation.value for observation in self._observations],
                self._config.method,
                self._config.threshold,
                self._config.minimum_sample_size,
            )
            if detection.warning is not None:
                status = "SKIPPED"
                error_code = detection.warning
            else:
                for index in detection.indices:
                    anomaly_count += 1
                    if len(references) < self._config.sample_output_limit:
                        references.append(anomaly_reference(self._observations[index]))
        if self._config.null_policy.value == "FAIL":
            anomaly_count += self._invalid_count
            references.extend(
                anomaly_reference(observation)
                for observation in self._invalid_references[
                    : max(0, self._config.sample_output_limit - len(references))
                ]
            )
        if anomaly_count and status not in {"ERROR", "SKIPPED"}:
            status = "ANOMALIES_FOUND"
        duration_ms = max(0.0, (self._monotonic() - self._started_at) * 1000)
        evaluated = self._total_numeric + (
            self._invalid_count if self._config.null_policy.value == "FAIL" else 0
        )
        return {
            "schemaVersion": "anomaly.report.v1",
            "column": self._config.column,
            "method": self._config.method.value,
            "threshold": self._config.threshold,
            "status": status,
            "errorCode": error_code,
            "minimumSampleSize": self._config.minimum_sample_size,
            "totalNumericCount": self._total_numeric,
            "numericSampleCount": len(self._observations),
            "evaluatedRowCount": evaluated,
            "anomalyCount": anomaly_count,
            "sampleOutputLimit": self._config.sample_output_limit,
            "anomalyReferences": references[: self._config.sample_output_limit],
            "warnings": warnings,
            "durationMs": round(duration_ms, 3),
            "artifactRef": None,
        }

    def _record_observation(self, row_number: int, value: float) -> None:
        observation = AnomalyObservation(row_number, value, row_hash(self._config.column, value))
        if len(self._observations) < self._max_retained_values:
            self._observations.append(observation)
            return
        slot = (row_number * 2_654_435_761) % self._total_numeric
        if slot < self._max_retained_values:
            self._observations[slot] = observation

    def _record_invalid(self, row_number: int, value: object) -> None:
        if len(self._invalid_references) >= self._config.sample_output_limit:
            return
        self._invalid_references.append(
            AnomalyObservation(row_number, 0.0, row_hash(self._config.column, json_safe(value)))
        )


def _numeric_value(value: object) -> float | None:
    if isinstance(value, bool) or value is None or not isinstance(value, (int, float, str)):
        return None
    try:
        number = float(value)
    except (TypeError, ValueError):
        return None
    return number if math.isfinite(number) else None

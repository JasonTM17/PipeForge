"""Chunk-aware quality rule aggregation with bounded failure references."""

from __future__ import annotations

import hashlib
import json
import time
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field
from typing import Any

from pipeforge_worker.contracts.quality import QualityRule, QualityRuleType, QualityStatus
from pipeforge_worker.readers.models import DataChunk, ReaderMetadata, ReaderStatsSnapshot
from pipeforge_worker.validators.rules import RowRuleEvaluator, RuleEvaluationError

MAX_FAILURE_REFERENCES = 128


@dataclass
class _RuleState:
    rule: QualityRule
    evaluator: RowRuleEvaluator | None = None
    passed: int = 0
    failed: int = 0
    error_code: str | None = None
    failure_references: list[dict[str, object]] = field(default_factory=list)


class QualityAggregator:
    """Evaluate immutable rules over chunks without retaining complete rows."""

    def __init__(
        self,
        rules: tuple[QualityRule, ...],
        *,
        monotonic: Callable[[], float] | None = None,
        max_failure_references: int = MAX_FAILURE_REFERENCES,
    ) -> None:
        if not rules:
            raise ValueError("at least one quality rule is required")
        if not 1 <= max_failure_references <= 10_000:
            raise ValueError("max_failure_references is outside the supported bound")
        self._rules = rules
        self._monotonic = monotonic or time.monotonic
        self._max_failure_references = max_failure_references
        self._metadata: ReaderMetadata | None = None
        self._states: list[_RuleState] = []
        self._schema_initialized = False
        self._rows = 0
        self._started_at = 0.0

    def start(self, metadata: ReaderMetadata) -> None:
        self._metadata = metadata
        self._started_at = self._monotonic()
        self._states = []
        self._schema_initialized = False
        if metadata.columns:
            self._initialize_states(metadata.columns)

    def _initialize_states(self, columns: tuple[str, ...]) -> None:
        if self._schema_initialized:
            return
        available = set(columns)
        self._states = []
        for rule in self._rules:
            state = _RuleState(rule)
            if not rule.enabled:
                self._states.append(state)
                continue
            missing = [column for column in rule.column_scope if column not in available]
            if missing:
                state.error_code = "MISSING_COLUMN"
            elif rule.rule_type is not QualityRuleType.ROW_COUNT_BETWEEN:
                try:
                    state.evaluator = RowRuleEvaluator(rule)
                except RuleEvaluationError as exc:
                    state.error_code = exc.code
            self._states.append(state)
        self._schema_initialized = True

    def consume(self, chunk: DataChunk) -> None:
        if self._metadata is None:
            raise RuntimeError("quality aggregator was not started")
        rows = list(chunk.frame.iter_rows(named=True))
        if not self._schema_initialized:
            self._metadata.columns = tuple(chunk.frame.columns)
            self._initialize_states(self._metadata.columns)
        self._rows += len(rows)
        for offset, row in enumerate(rows):
            for state in self._states:
                if state.rule.enabled is False or state.error_code is not None:
                    continue
                if state.evaluator is None:
                    continue
                try:
                    passed = state.evaluator.evaluate(row)
                except RuleEvaluationError as exc:
                    state.error_code = exc.code
                    continue
                if passed:
                    state.passed += 1
                else:
                    state.failed += 1
                    _record_failure(
                        state, row, chunk.row_start + offset + 1, self._max_failure_references
                    )

    def finish(self) -> Mapping[str, object]:
        return self._finish(None)

    def finish_with_stats(self, stats: ReaderStatsSnapshot) -> Mapping[str, object]:
        return self._finish(stats)

    def _finish(self, stats: ReaderStatsSnapshot | None) -> Mapping[str, object]:
        if self._metadata is None:
            raise RuntimeError("quality aggregator was not started")
        if not self._schema_initialized:
            self._initialize_states(self._metadata.columns)
        duration_ms = max(0.0, (self._monotonic() - self._started_at) * 1000)
        results = [_result(state, self._rows, duration_ms) for state in self._states]
        warnings = []
        if stats is not None and stats.malformed_rows:
            warnings.append("input contained malformed rows")
        return {
            "schemaVersion": "quality.report.v1",
            "dataset": {
                "rowCount": self._rows,
                "columns": list(self._metadata.columns),
                "malformedRowCount": stats.malformed_rows if stats is not None else 0,
            },
            "rules": results,
            "warnings": warnings,
        }


def _result(state: _RuleState, rows: int, duration_ms: float) -> dict[str, object]:
    rule = state.rule
    if not rule.enabled:
        status = QualityStatus.SKIPPED
        passed = failed = 0
        error_code = None
    elif rule.rule_type is QualityRuleType.ROW_COUNT_BETWEEN:
        minimum = _configuration_number(rule.configuration, "min")
        maximum = _configuration_number(rule.configuration, "max")
        passed = rows if minimum <= rows <= maximum else 0
        failed = 0 if passed else rows
        status = QualityStatus.PASSED if failed == 0 else QualityStatus.FAILED
        error_code = None
    elif state.error_code is not None:
        status = QualityStatus.ERROR
        passed = state.passed
        failed = state.failed
        error_code = state.error_code
    else:
        status = QualityStatus.PASSED if state.failed == 0 else QualityStatus.FAILED
        passed = state.passed
        failed = state.failed
        error_code = None
    evaluated = passed + failed
    return {
        "id": rule.rule_id,
        "name": rule.name,
        "ruleType": rule.rule_type.value,
        "severity": rule.severity.value,
        "status": status.value,
        "passedRowCount": passed,
        "failedRowCount": failed,
        "evaluatedRowCount": evaluated,
        "failurePercentage": round((failed / evaluated) * 100, 6) if evaluated else 0.0,
        "durationMs": round(duration_ms, 3),
        "errorCode": error_code,
        "failureReferences": list(state.failure_references),
        "artifactRef": None,
    }


def _record_failure(
    state: _RuleState,
    row: Mapping[str, Any],
    row_number: int,
    limit: int,
) -> None:
    if len(state.failure_references) >= limit:
        return
    values = {column: _json_safe(row.get(column)) for column in state.rule.column_scope}
    encoded = json.dumps(values, sort_keys=True, separators=(",", ":"), default=str).encode()
    state.failure_references.append(
        {"rowNumber": row_number, "rowHash": hashlib.sha256(encoded).hexdigest()[:16]}
    )


def _configuration_number(configuration: Mapping[str, object], key: str) -> float:
    value = configuration.get(key)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"quality rule configuration {key} must be numeric")
    return float(value)


def _json_safe(value: object) -> object:
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    isoformat = getattr(value, "isoformat", None)
    if callable(isoformat):
        return str(isoformat())
    return str(value)

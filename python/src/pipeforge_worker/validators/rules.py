"""Pure row evaluators for the supported data-quality rule families."""

from __future__ import annotations

import math
import re
from collections.abc import Iterable, Mapping
from datetime import date, datetime

from pipeforge_worker.contracts.quality import QualityRule, QualityRuleType


class RuleEvaluationError(ValueError):
    """Raised when a bounded evaluator cannot continue safely."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


class RowRuleEvaluator:
    def __init__(self, rule: QualityRule) -> None:
        self.rule = rule
        self._seen: set[str] = set()
        self._max_tracked = _integer(rule.configuration, "maxTrackedValues", 1_000_000)
        self._regex = (
            _regex(rule.configuration) if rule.rule_type is QualityRuleType.REGEX else None
        )

    def evaluate(self, row: Mapping[str, object]) -> bool:
        rule_type = self.rule.rule_type
        if rule_type is QualityRuleType.UNIQUE:
            key = _stable_key(row.get(column) for column in self.rule.column_scope)
            if key in self._seen:
                return False
            if len(self._seen) >= self._max_tracked:
                raise RuleEvaluationError("UNIQUE_STATE_LIMIT_EXCEEDED")
            self._seen.add(key)
            return True
        value = row.get(self.rule.column_scope[0])
        config = self.rule.configuration
        if rule_type is QualityRuleType.NOT_NULL:
            return not _is_null(value)
        if rule_type is QualityRuleType.BETWEEN:
            numeric = _try_numeric(value)
            return numeric is not None and _number(config, "min") <= numeric <= _number(
                config, "max"
            )
        if rule_type is QualityRuleType.MIN_LENGTH:
            return isinstance(value, str) and len(value) >= _integer(config, "value", 0)
        if rule_type is QualityRuleType.MAX_LENGTH:
            return isinstance(value, str) and len(value) <= _integer(config, "value", 0)
        if rule_type is QualityRuleType.REGEX:
            return (
                isinstance(value, str)
                and len(value) <= 16_384
                and bool(self._regex and self._regex.fullmatch(value))
            )
        if rule_type is QualityRuleType.EMAIL_FORMAT:
            return (
                isinstance(value, str)
                and len(value) <= 320
                and bool(re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", value))
            )
        if rule_type is QualityRuleType.DATE_FORMAT:
            if not isinstance(value, str):
                return False
            try:
                datetime.strptime(value, _text(config, "format"))
            except ValueError:
                return False
            return True
        if rule_type in {QualityRuleType.ALLOWED_VALUES, QualityRuleType.REFERENTIAL_SET}:
            return _stable_value(value) in {_stable_value(item) for item in _values(config)}
        if rule_type is QualityRuleType.COLUMN_TYPE:
            return _matches_type(value, _text(config, "type"))
        raise RuleEvaluationError("UNSUPPORTED_RULE_TYPE")


def _is_null(value: object) -> bool:
    return (
        value is None
        or (isinstance(value, str) and not value.strip())
        or (isinstance(value, float) and math.isnan(value))
    )


def _try_numeric(value: object) -> float | None:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    number = float(value)
    if not math.isfinite(number):
        return None
    return number


def _matches_type(value: object, expected: str) -> bool:
    if expected == "string":
        return isinstance(value, str)
    if expected == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if expected == "number":
        return (
            isinstance(value, (int, float)) and not isinstance(value, bool) and not _is_null(value)
        )
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "date":
        return isinstance(value, date) and not isinstance(value, datetime)
    if expected == "datetime":
        return isinstance(value, datetime)
    return False


def _stable_key(values: Iterable[object]) -> str:
    return "|".join(_stable_value(value) for value in values)


def _stable_value(value: object) -> str:
    if value is None:
        return "null"
    if isinstance(value, datetime):
        return f"datetime:{value.isoformat()}"
    if isinstance(value, date):
        return f"date:{value.isoformat()}"
    if isinstance(value, float) and math.isnan(value):
        return "float:nan"
    return f"{type(value).__name__}:{value!s}"


def _regex(config: Mapping[str, object]) -> re.Pattern[str]:
    pattern = _text(config, "pattern")
    try:
        return re.compile(pattern)
    except re.error as exc:
        raise RuleEvaluationError("INVALID_REGEX") from exc


def _values(config: Mapping[str, object]) -> list[object]:
    value = config.get("values")
    if not isinstance(value, list):
        raise RuleEvaluationError("INVALID_VALUE_SET")
    return value


def _text(config: Mapping[str, object], key: str) -> str:
    value = config.get(key)
    if not isinstance(value, str):
        raise RuleEvaluationError("INVALID_RULE_CONFIGURATION")
    return value


def _number(config: Mapping[str, object], key: str) -> float:
    value = config.get(key)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise RuleEvaluationError("INVALID_RULE_CONFIGURATION")
    return float(value)


def _integer(config: Mapping[str, object], key: str, default: int) -> int:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise RuleEvaluationError("INVALID_RULE_CONFIGURATION")
    return value

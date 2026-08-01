"""Strict, serializable data-quality rule contracts."""

from __future__ import annotations

import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from enum import StrEnum

from pipeforge_worker.contracts.operations import OperationConfigError


class QualityRuleType(StrEnum):
    NOT_NULL = "NOT_NULL"
    UNIQUE = "UNIQUE"
    BETWEEN = "BETWEEN"
    MIN_LENGTH = "MIN_LENGTH"
    MAX_LENGTH = "MAX_LENGTH"
    REGEX = "REGEX"
    EMAIL_FORMAT = "EMAIL_FORMAT"
    DATE_FORMAT = "DATE_FORMAT"
    ALLOWED_VALUES = "ALLOWED_VALUES"
    COLUMN_TYPE = "COLUMN_TYPE"
    ROW_COUNT_BETWEEN = "ROW_COUNT_BETWEEN"
    REFERENTIAL_SET = "REFERENTIAL_SET"
    CUSTOM_EXPRESSION = "CUSTOM_EXPRESSION"


class QualitySeverity(StrEnum):
    INFO = "INFO"
    WARNING = "WARNING"
    ERROR = "ERROR"
    CRITICAL = "CRITICAL"


class QualityStatus(StrEnum):
    PASSED = "PASSED"
    FAILED = "FAILED"
    ERROR = "ERROR"
    SKIPPED = "SKIPPED"


@dataclass(frozen=True, slots=True)
class QualityRule:
    rule_id: str
    name: str
    column_scope: tuple[str, ...]
    rule_type: QualityRuleType
    configuration: Mapping[str, object]
    severity: QualitySeverity
    enabled: bool


def parse_quality_rules(value: object) -> tuple[QualityRule, ...]:
    if not isinstance(value, Sequence) or isinstance(value, (str, bytes)):
        raise OperationConfigError("quality rules must be a sequence")
    if not 1 <= len(value) <= 256:
        raise OperationConfigError("quality rules must contain between 1 and 256 rules")
    parsed = tuple(_parse_rule(raw, index) for index, raw in enumerate(value))
    ids = [rule.rule_id for rule in parsed]
    if len(set(ids)) != len(ids):
        raise OperationConfigError("quality rule ids must be unique")
    return parsed


def _parse_rule(raw: object, index: int) -> QualityRule:
    if not isinstance(raw, Mapping):
        raise OperationConfigError(f"quality rule {index} must be an object")
    allowed = {
        "id",
        "name",
        "columnScope",
        "ruleType",
        "configuration",
        "severity",
        "enabled",
    }
    unknown = set(raw) - allowed
    if unknown:
        raise OperationConfigError(f"quality rule {index} has unknown fields: {sorted(unknown)}")
    rule_id = _text(raw.get("id"), f"quality rule {index}.id", 128)
    name = _text(raw.get("name"), f"quality rule {index}.name", 120)
    columns = _columns(raw.get("columnScope"), index)
    try:
        rule_type = QualityRuleType(str(raw["ruleType"]))
    except (KeyError, ValueError) as exc:
        raise OperationConfigError(f"quality rule {index}.ruleType is unsupported") from exc
    try:
        severity = QualitySeverity(str(raw.get("severity", QualitySeverity.ERROR)))
    except ValueError as exc:
        raise OperationConfigError(f"quality rule {index}.severity is unsupported") from exc
    enabled = raw.get("enabled", True)
    if not isinstance(enabled, bool):
        raise OperationConfigError(f"quality rule {index}.enabled must be boolean")
    configuration = raw.get("configuration", {})
    if not isinstance(configuration, Mapping):
        raise OperationConfigError(f"quality rule {index}.configuration must be an object")
    normalized = {str(key): item for key, item in configuration.items()}
    _validate_rule_config(rule_type, columns, normalized, index)
    return QualityRule(rule_id, name, columns, rule_type, normalized, severity, enabled)


def _validate_rule_config(
    rule_type: QualityRuleType,
    columns: tuple[str, ...],
    config: Mapping[str, object],
    index: int,
) -> None:
    if rule_type is QualityRuleType.CUSTOM_EXPRESSION:
        raise OperationConfigError("CUSTOM_EXPRESSION is disabled; no code execution is supported")
    column_count = len(columns)
    if rule_type is QualityRuleType.ROW_COUNT_BETWEEN:
        _require_columns(column_count, 0, 0, rule_type, index)
    else:
        maximum = 16 if rule_type is QualityRuleType.UNIQUE else 1
        _require_columns(column_count, 1, maximum, rule_type, index)
    allowed: dict[QualityRuleType, set[str]] = {
        QualityRuleType.NOT_NULL: set(),
        QualityRuleType.UNIQUE: {"maxTrackedValues"},
        QualityRuleType.BETWEEN: {"min", "max"},
        QualityRuleType.MIN_LENGTH: {"value"},
        QualityRuleType.MAX_LENGTH: {"value"},
        QualityRuleType.REGEX: {"pattern"},
        QualityRuleType.EMAIL_FORMAT: set(),
        QualityRuleType.DATE_FORMAT: {"format"},
        QualityRuleType.ALLOWED_VALUES: {"values"},
        QualityRuleType.COLUMN_TYPE: {"type"},
        QualityRuleType.ROW_COUNT_BETWEEN: {"min", "max"},
        QualityRuleType.REFERENTIAL_SET: {"values"},
        QualityRuleType.CUSTOM_EXPRESSION: set(),
    }
    unknown = set(config) - allowed[rule_type]
    if unknown:
        raise OperationConfigError(f"quality rule {index} has unknown configuration fields")
    if rule_type in {QualityRuleType.BETWEEN, QualityRuleType.ROW_COUNT_BETWEEN}:
        lower = _number(config.get("min"), f"quality rule {index}.min")
        upper = _number(config.get("max"), f"quality rule {index}.max")
        if lower > upper:
            raise OperationConfigError(f"quality rule {index} min must not exceed max")
    if rule_type in {QualityRuleType.MIN_LENGTH, QualityRuleType.MAX_LENGTH}:
        _integer(config.get("value"), f"quality rule {index}.value", 0, 1_000_000)
    if rule_type is QualityRuleType.UNIQUE and "maxTrackedValues" in config:
        _integer(config["maxTrackedValues"], f"quality rule {index}.maxTrackedValues", 1, 2_000_000)
    if rule_type is QualityRuleType.REGEX:
        pattern = _text(config.get("pattern"), f"quality rule {index}.pattern", 512)
        _compile_safe_regex(pattern, index)
    if rule_type is QualityRuleType.DATE_FORMAT:
        _text(config.get("format"), f"quality rule {index}.format", 64)
    if rule_type in {QualityRuleType.ALLOWED_VALUES, QualityRuleType.REFERENTIAL_SET}:
        _scalar_values(config.get("values"), index)
    if rule_type is QualityRuleType.COLUMN_TYPE:
        expected = _text(config.get("type"), f"quality rule {index}.type", 16)
        if expected not in {"string", "integer", "number", "boolean", "date", "datetime"}:
            raise OperationConfigError(f"quality rule {index}.type is unsupported")


def _require_columns(
    count: int, minimum: int, maximum: int, rule_type: QualityRuleType, index: int
) -> None:
    if not minimum <= count <= maximum:
        raise OperationConfigError(
            f"quality rule {index} {rule_type.value} requires {minimum} to {maximum} columns"
        )


def _columns(value: object, index: int) -> tuple[str, ...]:
    if not isinstance(value, list) or len(value) > 256:
        raise OperationConfigError(f"quality rule {index}.columnScope must be a list")
    result: list[str] = []
    for column in value:
        if not isinstance(column, str) or not _is_safe_column(column):
            raise OperationConfigError(
                f"quality rule {index}.columnScope contains an invalid column"
            )
        result.append(column)
    if len(set(result)) != len(result):
        raise OperationConfigError(f"quality rule {index}.columnScope must not contain duplicates")
    return tuple(result)


def _text(value: object, name: str, maximum: int) -> str:
    if not isinstance(value, str):
        raise OperationConfigError(f"{name} must be a string")
    normalized = value.strip()
    if not 1 <= len(normalized) <= maximum or any(char in normalized for char in "\r\n"):
        raise OperationConfigError(f"{name} is outside the supported bound")
    return normalized


def _number(value: object, name: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise OperationConfigError(f"{name} must be numeric")
    result = float(value)
    if result != result or result in {float("inf"), float("-inf")}:
        raise OperationConfigError(f"{name} must be finite")
    return result


def _integer(value: object, name: str, minimum: int, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise OperationConfigError(f"{name} is outside the supported bound")
    return value


def _scalar_values(value: object, index: int) -> None:
    if not isinstance(value, list) or not 1 <= len(value) <= 10_000:
        raise OperationConfigError(f"quality rule {index}.values must contain 1 to 10000 items")
    if any(not _is_json_scalar(item) for item in value):
        raise OperationConfigError(f"quality rule {index}.values must contain scalar values")


def _is_json_scalar(value: object) -> bool:
    return value is None or isinstance(value, (str, int, float, bool))


def _compile_safe_regex(pattern: str, index: int) -> None:
    # Grouping, alternation, lookarounds, and backreferences are intentionally
    # excluded. This small subset keeps matching bounded and predictable.
    if any(token in pattern for token in ("(", ")", "|", "\\1", "\\2", "\\g<")):
        raise OperationConfigError(f"quality rule {index}.pattern uses an unsafe regex construct")
    try:
        re.compile(pattern)
    except re.error as exc:
        raise OperationConfigError(f"quality rule {index}.pattern is invalid") from exc


def _is_safe_column(value: str) -> bool:
    letters = "_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    characters = f"{letters}0123456789"
    return (
        bool(value)
        and len(value) <= 128
        and value[0] in letters
        and all(char in characters for char in value)
    )

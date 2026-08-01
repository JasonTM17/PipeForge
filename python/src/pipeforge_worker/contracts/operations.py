"""Strict operation requests shared by the processing pipeline."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from enum import StrEnum
from typing import Any


class OperationType(StrEnum):
    PROFILE_DATASET = "PROFILE_DATASET"
    CHECK_MISSING_VALUES = "CHECK_MISSING_VALUES"
    CHECK_DUPLICATES = "CHECK_DUPLICATES"
    DETECT_OUTLIERS = "DETECT_OUTLIERS"
    VALIDATE_QUALITY = "VALIDATE_QUALITY"


class OperationConfigError(ValueError):
    """Raised when an operation is unsafe or cannot be dispatched."""


@dataclass(frozen=True, slots=True)
class OperationRequest:
    type: OperationType
    config: Mapping[str, object]


def parse_operations(value: Sequence[Mapping[str, Any]]) -> tuple[OperationRequest, ...]:
    if not isinstance(value, Sequence) or isinstance(value, (str, bytes)):
        raise OperationConfigError("operations must be a sequence")
    if not 1 <= len(value) <= 32:
        raise OperationConfigError("operations must contain between 1 and 32 items")

    parsed: list[OperationRequest] = []
    for index, raw in enumerate(value):
        if not isinstance(raw, Mapping):
            raise OperationConfigError(f"operation {index} must be an object")
        try:
            operation_type = OperationType(str(raw["type"]))
        except (KeyError, ValueError) as exc:
            raise OperationConfigError(f"operation {index} has an unsupported type") from exc
        config = raw.get("config")
        if not isinstance(config, Mapping):
            raise OperationConfigError(f"operation {index}.config must be an object")
        normalized = {str(key): value for key, value in config.items()}
        _validate_config(operation_type, normalized, index)
        parsed.append(OperationRequest(type=operation_type, config=normalized))
    return tuple(parsed)


def _validate_config(
    operation_type: OperationType,
    config: Mapping[str, object],
    index: int,
) -> None:
    if operation_type is OperationType.PROFILE_DATASET:
        _validate_profile_config(config, index)
        return
    if operation_type is OperationType.CHECK_MISSING_VALUES:
        _validate_columns(config, "columns", operation_type, index)
        return
    if operation_type is OperationType.CHECK_DUPLICATES:
        _validate_columns(config, "keyColumns", operation_type, index)
        return
    if operation_type is OperationType.DETECT_OUTLIERS:
        _require_keys(config, {"column", "method"}, operation_type, index)
        column = config["column"]
        method = config["method"]
        if not isinstance(column, str) or not _is_safe_column(column):
            raise OperationConfigError(f"operation {index}.column is invalid")
        if method not in {"IQR", "Z_SCORE"}:
            raise OperationConfigError(f"operation {index}.method is invalid")
        return
    if operation_type is OperationType.VALIDATE_QUALITY:
        _validate_quality_config(config, index)


def _validate_columns(
    config: Mapping[str, object], key: str, operation_type: OperationType, index: int
) -> None:
    _require_keys(config, {key}, operation_type, index)
    columns = config[key]
    if not isinstance(columns, list) or not 1 <= len(columns) <= 256:
        raise OperationConfigError(f"operation {index}.{key} must contain 1 to 256 columns")
    if any(not isinstance(column, str) or not _is_safe_column(column) for column in columns):
        raise OperationConfigError(f"operation {index}.{key} contains an invalid column")
    if len(set(columns)) != len(columns):
        raise OperationConfigError(f"operation {index}.{key} must not contain duplicates")


def _validate_profile_config(config: Mapping[str, object], index: int) -> None:
    allowed = {
        "sampling",
        "includeCommonValues",
        "maxCommonValues",
        "quantiles",
        "distinctStrategy",
        "maxDistinctValues",
        "maxMemoryBytes",
        "sensitiveColumns",
    }
    unknown = set(config) - allowed
    if unknown:
        raise OperationConfigError(
            f"operation {index} PROFILE_DATASET has unknown fields: {sorted(unknown)}"
        )
    sampling = config.get("sampling")
    if sampling is not None:
        if not isinstance(sampling, Mapping):
            raise OperationConfigError(f"operation {index}.sampling must be an object")
        unknown_sampling = set(sampling) - {"strategy", "maxRows", "seed"}
        if unknown_sampling:
            raise OperationConfigError(
                f"operation {index}.sampling has unknown fields: {sorted(unknown_sampling)}"
            )
        if sampling.get("strategy", "RESERVOIR") != "RESERVOIR":
            raise OperationConfigError(f"operation {index}.sampling.strategy is invalid")
        _bounded_int(sampling.get("maxRows", 2_048), 0, 100_000, "sampling.maxRows", index)
        _bounded_int(sampling.get("seed", 0), 0, 2**31 - 1, "sampling.seed", index)
    if "includeCommonValues" in config and not isinstance(config["includeCommonValues"], bool):
        raise OperationConfigError(f"operation {index}.includeCommonValues must be boolean")
    if "maxCommonValues" in config:
        _bounded_int(config["maxCommonValues"], 0, 100, "maxCommonValues", index)
    quantiles = config.get("quantiles")
    if quantiles is not None:
        if not isinstance(quantiles, list) or not 1 <= len(quantiles) <= 32:
            raise OperationConfigError(f"operation {index}.quantiles must contain 1 to 32 values")
        if any(
            isinstance(value, bool) or not isinstance(value, (int, float)) or not 0 <= value <= 1
            for value in quantiles
        ):
            raise OperationConfigError(f"operation {index}.quantiles must be between 0 and 1")
        if len(set(quantiles)) != len(quantiles):
            raise OperationConfigError(f"operation {index}.quantiles must not contain duplicates")
    if "distinctStrategy" in config and config["distinctStrategy"] not in {"EXACT", "APPROXIMATE"}:
        raise OperationConfigError(f"operation {index}.distinctStrategy is invalid")
    if "maxDistinctValues" in config:
        _bounded_int(config["maxDistinctValues"], 128, 1_000_000, "maxDistinctValues", index)
    if "maxMemoryBytes" in config:
        _bounded_int(config["maxMemoryBytes"], 1 << 20, 1 << 32, "maxMemoryBytes", index)
    if "sensitiveColumns" in config:
        sensitive = config["sensitiveColumns"]
        if not isinstance(sensitive, list) or len(sensitive) > 256:
            raise OperationConfigError(f"operation {index}.sensitiveColumns is invalid")
        if any(not isinstance(column, str) or not _is_safe_column(column) for column in sensitive):
            raise OperationConfigError(
                f"operation {index}.sensitiveColumns contains an invalid column"
            )


def _validate_quality_config(config: Mapping[str, object], index: int) -> None:
    _require_keys(config, {"rules"}, OperationType.VALIDATE_QUALITY, index)
    rules = config["rules"]
    if not isinstance(rules, list) or not 1 <= len(rules) <= 256:
        raise OperationConfigError(f"operation {index}.rules must contain 1 to 256 rules")
    if any(not isinstance(rule, Mapping) for rule in rules):
        raise OperationConfigError(f"operation {index}.rules must contain objects")


def _bounded_int(value: object, minimum: int, maximum: int, name: str, index: int) -> None:
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise OperationConfigError(f"operation {index}.{name} is outside the supported bound")


def _require_keys(
    config: Mapping[str, object], required: set[str], operation_type: OperationType, index: int
) -> None:
    unknown = set(config) - required
    missing = required - set(config)
    if unknown or missing:
        raise OperationConfigError(
            f"operation {index} {operation_type}: "
            f"unknown={sorted(unknown)}, missing={sorted(missing)}"
        )


def _is_safe_column(value: str) -> bool:
    letters = "_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    characters = f"{letters}0123456789"
    return (
        bool(value)
        and len(value) <= 128
        and value[0] in letters
        and all(char in characters for char in value)
    )

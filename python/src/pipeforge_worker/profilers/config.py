"""Validated profile operation configuration."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass

from pipeforge_worker.contracts.operations import OperationConfigError


@dataclass(frozen=True, slots=True)
class ProfileConfig:
    sample_size: int = 2_048
    sample_seed: int = 0
    include_common_values: bool = False
    max_common_values: int = 10
    quantiles: tuple[float, ...] = (0.25, 0.5, 0.75)
    distinct_strategy: str = "EXACT"
    max_distinct_values: int = 10_000
    max_memory_bytes: int = 256 * 1024 * 1024
    sensitive_columns: frozenset[str] = frozenset()

    @classmethod
    def from_mapping(cls, value: Mapping[str, object]) -> ProfileConfig:
        sampling = value.get("sampling", {})
        if not isinstance(sampling, Mapping):
            raise OperationConfigError("profile sampling must be an object")
        strategy = sampling.get("strategy", "RESERVOIR")
        if strategy != "RESERVOIR":
            raise OperationConfigError("profile sampling strategy is invalid")
        sample_size = _int_value(sampling.get("maxRows", 2_048), "sampling.maxRows", 0, 100_000)
        sample_seed = _int_value(sampling.get("seed", 0), "sampling.seed", 0, 2**31 - 1)
        max_common = _int_value(value.get("maxCommonValues", 10), "maxCommonValues", 0, 100)
        quantiles = _quantiles(value.get("quantiles", (0.25, 0.5, 0.75)))
        distinct_strategy = str(value.get("distinctStrategy", "EXACT"))
        if distinct_strategy not in {"EXACT", "APPROXIMATE"}:
            raise OperationConfigError("profile distinct strategy is invalid")
        max_distinct = _int_value(
            value.get("maxDistinctValues", 10_000), "maxDistinctValues", 128, 1_000_000
        )
        max_memory = _int_value(
            value.get("maxMemoryBytes", 256 * 1024 * 1024),
            "maxMemoryBytes",
            1 << 20,
            1 << 32,
        )
        include_common_values = value.get("includeCommonValues", False)
        if not isinstance(include_common_values, bool):
            raise OperationConfigError("profile includeCommonValues must be boolean")
        sensitive = value.get("sensitiveColumns", [])
        if not isinstance(sensitive, list) or any(
            not isinstance(item, str) or not _is_safe_column(item) for item in sensitive
        ):
            raise OperationConfigError("profile sensitiveColumns must be a string list")
        return cls(
            sample_size=sample_size,
            sample_seed=sample_seed,
            include_common_values=include_common_values,
            max_common_values=max_common,
            quantiles=quantiles,
            distinct_strategy=distinct_strategy,
            max_distinct_values=max_distinct,
            max_memory_bytes=max_memory,
            sensitive_columns=frozenset(sensitive),
        )


def _int_value(value: object, name: str, minimum: int, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise OperationConfigError(f"profile {name} is outside the supported bound")
    return value


def _quantiles(value: object) -> tuple[float, ...]:
    if not isinstance(value, (list, tuple)) or not 1 <= len(value) <= 32:
        raise OperationConfigError("profile quantiles must contain 1 to 32 values")
    try:
        normalized = tuple(float(item) for item in value)
    except (TypeError, ValueError) as exc:
        raise OperationConfigError("profile quantiles must contain numbers") from exc
    if any(item < 0 or item > 1 for item in normalized) or len(set(normalized)) != len(normalized):
        raise OperationConfigError("profile quantiles must be unique values between 0 and 1")
    return normalized


def _is_safe_column(value: str) -> bool:
    letters = "_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    characters = f"{letters}0123456789"
    return (
        bool(value)
        and len(value) <= 128
        and value[0] in letters
        and all(char in characters for char in value)
    )

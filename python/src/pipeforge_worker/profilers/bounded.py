"""Bounded deterministic sampling, distinct counting, and common values."""

from __future__ import annotations

import hashlib
import json
import math
import random
from datetime import date, datetime
from typing import Any


class ReservoirSampler:
    def __init__(self, capacity: int, seed: int) -> None:
        self._capacity = capacity
        self._random = random.Random(seed)
        self._seen = 0
        self.values: list[float] = []

    def add(self, value: float) -> None:
        if self._capacity == 0:
            return
        self._seen += 1
        if len(self.values) < self._capacity:
            self.values.append(value)
            return
        index = self._random.randrange(self._seen)
        if index < self._capacity:
            self.values[index] = value

    @property
    def sampled(self) -> bool:
        return self._seen > len(self.values)


class DistinctCounter:
    def __init__(self, limit: int, strategy: str) -> None:
        self._limit = limit
        self._strategy = strategy
        self._exact: set[int] = set()
        self._hashes: set[int] = set()
        self.approximate = strategy == "APPROXIMATE"

    def add(self, value: Any) -> None:
        canonical = json.dumps(_json_safe(value), ensure_ascii=True, sort_keys=True)
        digest = int.from_bytes(hashlib.sha256(canonical.encode()).digest()[:8], "big")
        if not self.approximate:
            self._exact.add(digest)
            if len(self._exact) > self._limit:
                self.approximate = True
                self._hashes = set(self._exact)
                self._exact.clear()
        else:
            self._hashes.add(digest)
            if len(self._hashes) > self._limit:
                self._hashes.remove(max(self._hashes))

    def count(self) -> int:
        if not self.approximate:
            return len(self._exact)
        if not self._hashes:
            return 0
        if len(self._hashes) < self._limit:
            return len(self._hashes)
        maximum = max(self._hashes)
        if maximum == 0:
            return len(self._hashes)
        estimate = (len(self._hashes) - 1) * ((1 << 64) - 1) / maximum
        return max(len(self._hashes), round(estimate))


class BoundedFrequencies:
    def __init__(self, limit: int) -> None:
        self._limit = limit
        self._tracked_limit = max(limit * 4, 16)
        self._values: dict[str, tuple[Any, int]] = {}
        self.truncated = False

    def add(self, value: Any) -> None:
        if self._limit == 0:
            return
        safe_value = _json_safe(value)
        key = json.dumps(safe_value, ensure_ascii=True, sort_keys=True)
        current = self._values.get(key)
        if current is not None:
            self._values[key] = (current[0], current[1] + 1)
            return
        if len(self._values) >= self._tracked_limit:
            self.truncated = True
            keep = sorted(self._values.items(), key=lambda item: (-item[1][1], item[0]))[
                : self._tracked_limit // 2
            ]
            self._values = dict(keep)
        self._values[key] = (safe_value, 1)

    def top(self) -> list[dict[str, object]]:
        values = sorted(self._values.values(), key=lambda item: (-item[1], repr(item[0])))
        return [{"value": value, "count": count} for value, count in values[: self._limit]]


def _json_safe(value: Any) -> Any:
    if isinstance(value, float) and not math.isfinite(value):
        return None
    if value is None or isinstance(value, (bool, int, float)):
        return value
    if isinstance(value, (datetime, date)):
        return value.isoformat()
    if isinstance(value, str):
        return value[:256]
    if isinstance(value, (list, tuple)):
        return [_json_safe(item) for item in value[:32]]
    if isinstance(value, dict):
        return {str(key)[:128]: _json_safe(item) for key, item in list(value.items())[:32]}
    return str(value)[:256]

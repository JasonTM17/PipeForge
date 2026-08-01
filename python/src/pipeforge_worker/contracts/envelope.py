"""Strict envelope representation shared by worker publishers and consumers."""

from __future__ import annotations

import json
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from uuid import UUID, uuid4


@dataclass(frozen=True, slots=True)
class Envelope:
    message_id: UUID
    message_type: str
    schema_version: int
    occurred_at: datetime
    trace_id: str
    correlation_id: str
    causation_id: str
    payload: Mapping[str, Any]

    def to_mapping(self) -> dict[str, Any]:
        return {
            "messageId": str(self.message_id),
            "messageType": self.message_type,
            "schemaVersion": self.schema_version,
            "occurredAt": self.occurred_at.astimezone(UTC).isoformat().replace("+00:00", "Z"),
            "traceId": self.trace_id,
            "correlationId": self.correlation_id,
            "causationId": self.causation_id,
            "payload": dict(self.payload),
        }

    def to_bytes(self) -> bytes:
        return json.dumps(
            self.to_mapping(), ensure_ascii=True, separators=(",", ":"), sort_keys=True
        ).encode("utf-8")

    @classmethod
    def from_mapping(cls, value: Mapping[str, Any]) -> Envelope:
        return cls(
            message_id=UUID(str(value["messageId"])),
            message_type=str(value["messageType"]),
            schema_version=int(value["schemaVersion"]),
            occurred_at=_parse_datetime(str(value["occurredAt"])),
            trace_id=str(value["traceId"]),
            correlation_id=str(value["correlationId"]),
            causation_id=str(value["causationId"]),
            payload=dict(value["payload"]),
        )


def build_envelope(
    message_type: str,
    trace_id: str,
    correlation_id: str,
    causation_id: str,
    payload: Mapping[str, Any],
    clock: Callable[[], datetime] | None = None,
) -> Envelope:
    now = (clock or (lambda: datetime.now(UTC)))().astimezone(UTC)
    return Envelope(
        message_id=uuid4(),
        message_type=message_type,
        schema_version=1,
        occurred_at=now,
        trace_id=trace_id,
        correlation_id=correlation_id,
        causation_id=causation_id,
        payload=dict(payload),
    )


def _parse_datetime(value: str) -> datetime:
    normalized = value[:-1] + "+00:00" if value.endswith("Z") else value
    return datetime.fromisoformat(normalized).astimezone(UTC)

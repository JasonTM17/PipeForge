"""Structured logging with conservative redaction at the formatting boundary."""

from __future__ import annotations

import json
import logging
import re
import sys
from collections.abc import Mapping
from typing import Any

_SENSITIVE_KEY_PARTS = ("password", "secret", "token", "authorization", "presigned", "credential")
_URI_CREDENTIALS = re.compile(r"(?i)(://[^:/\s]+:)[^@/\s]+@")
_BEARER = re.compile(r"(?i)(bearer\s+)[^\s]+")


def redact_value(key: str, value: Any) -> Any:
    lowered = key.lower()
    if any(part in lowered for part in _SENSITIVE_KEY_PARTS):
        return "[REDACTED]"
    if isinstance(value, Mapping):
        return {
            str(child_key): redact_value(str(child_key), child_value)
            for child_key, child_value in value.items()
        }
    if isinstance(value, (list, tuple)):
        return [redact_value(key, item) for item in value]
    if isinstance(value, str):
        return _BEARER.sub(r"\1[REDACTED]", _URI_CREDENTIALS.sub(r"\1[REDACTED]@", value))
    return value


def redact_fields(fields: Mapping[str, Any]) -> dict[str, Any]:
    return {str(key): redact_value(str(key), value) for key, value in fields.items()}


class JsonFormatter(logging.Formatter):
    """Emit a compact JSON record without serializing exception internals by default."""

    def format(self, record: logging.LogRecord) -> str:
        fields = getattr(record, "pipeforge_fields", {})
        payload: dict[str, Any] = {
            "timestamp": self.formatTime(record, "%Y-%m-%dT%H:%M:%S%z"),
            "level": record.levelname,
            "logger": record.name,
            "message": redact_value("message", record.getMessage()),
        }
        if isinstance(fields, Mapping):
            payload.update(redact_fields(fields))
        if record.exc_info and record.exc_info[0] is not None:
            payload["exception"] = record.exc_info[0].__name__
        return json.dumps(payload, ensure_ascii=True, separators=(",", ":"), default=str)


def configure_logging(level: str) -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JsonFormatter())
    root = logging.getLogger()
    root.handlers.clear()
    root.addHandler(handler)
    root.setLevel(level)


def log_event(logger: logging.Logger, level: int, event: str, **fields: Any) -> None:
    logger.log(level, event, extra={"pipeforge_fields": {"event": event, **fields}})

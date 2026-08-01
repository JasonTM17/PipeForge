"""JSON Schema validation against the repository's versioned contract files."""

from __future__ import annotations

import json
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource


class ContractValidationError(ValueError):
    """A message failed the shared envelope or payload schema."""


class ContractValidator:
    def __init__(self, schema_dir: Path, max_message_bytes: int = 1 << 20) -> None:
        self.schema_dir = schema_dir
        self.max_message_bytes = max_message_bytes
        self._validators = self._load_validators()

    def validate_bytes(self, body: bytes) -> dict[str, Any]:
        if len(body) > self.max_message_bytes:
            raise ContractValidationError("message exceeds configured size limit")
        try:
            value = json.loads(
                body.decode("utf-8"),
                object_pairs_hook=_reject_duplicate_keys,
                parse_constant=_reject_non_json_number,
            )
        except (UnicodeDecodeError, json.JSONDecodeError, ValueError) as exc:
            raise ContractValidationError(f"message is not valid JSON: {exc}") from exc
        if not isinstance(value, dict):
            raise ContractValidationError("message must be a JSON object")
        return self.validate_mapping(value)

    def validate_mapping(self, value: Mapping[str, Any]) -> dict[str, Any]:
        message_type = value.get("messageType")
        schema_version = value.get("schemaVersion")
        if not isinstance(message_type, str) or not isinstance(schema_version, int):
            raise ContractValidationError("messageType and schemaVersion are required")
        key = f"{message_type}.v{schema_version}"
        validator = self._validators.get(key)
        if validator is None:
            raise ContractValidationError(f"unsupported message contract: {key}")
        errors = sorted(validator.iter_errors(value), key=lambda error: list(error.path))
        if errors:
            first = errors[0]
            location = ".".join(str(part) for part in first.path) or "message"
            raise ContractValidationError(f"{location}: {first.message}")
        return dict(value)

    def _load_validators(self) -> dict[str, Draft202012Validator]:
        if not self.schema_dir.is_dir():
            raise ContractValidationError(
                f"contract schema directory does not exist: {self.schema_dir}"
            )
        schemas: dict[str, dict[str, Any]] = {}
        for path in sorted(self.schema_dir.glob("*.schema.json")):
            with path.open(encoding="utf-8") as stream:
                schemas[path.name] = json.load(stream)
        resources = {
            key: Resource.from_contents(schema)
            for key, schema in schemas.items()
            if isinstance(schema.get("$id"), str)
        }
        registry = Registry().with_resources(resources.items())
        validators: dict[str, Draft202012Validator] = {}
        for filename, schema in schemas.items():
            if not filename.endswith(".schema.json"):
                continue
            key = filename.removesuffix(".schema.json")
            validators[key] = Draft202012Validator(
                schema, registry=registry, format_checker=FormatChecker()
            )
        return validators


def _reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def _reject_non_json_number(value: str) -> None:
    raise ValueError(f"invalid JSON number constant: {value}")

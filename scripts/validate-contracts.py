#!/usr/bin/env python3
"""Validate every PipeForge contract schema and its positive/negative fixtures."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource


ROOT = Path(__file__).resolve().parents[1]
SCHEMA_DIR = ROOT / "contracts" / "json-schema"
EXAMPLE_DIR = ROOT / "contracts" / "examples"
VERSIONED_SCHEMA_NAME = re.compile(r"^[a-z0-9-]+(?:\.[a-z0-9-]+)*\.v\d+\.schema\.json$")
VERSIONED_EXAMPLE_NAME = re.compile(r"^[a-z0-9-]+(?:\.[a-z0-9-]+)*\.v\d+\.json$")


def load_json(path: Path) -> dict:
    with path.open(encoding="utf-8") as stream:
        value = json.load(stream)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: top-level JSON value must be an object")
    return value


def schema_store() -> dict[str, dict]:
    store: dict[str, dict] = {}
    for path in SCHEMA_DIR.glob("*.schema.json"):
        schema = load_json(path)
        store[path.name] = schema
        if "$id" in schema:
            store[schema["$id"]] = schema
    return store


def validator_for(schema_path: Path, store: dict[str, dict]) -> Draft202012Validator:
    schema = load_json(schema_path)
    resources = {
        key: Resource.from_contents(value)
        for key, value in store.items()
        if key.startswith("https://")
    }
    registry = Registry().with_resources(resources.items())
    return Draft202012Validator(schema, registry=registry, format_checker=FormatChecker())


def positive_schema_for(example_path: Path) -> Path:
    return SCHEMA_DIR / f"{example_path.stem}.schema.json"


def invalid_schema_for(example_path: Path) -> Path:
    prefix = example_path.name.split("-invalid-", 1)[0]
    return SCHEMA_DIR / f"{prefix}.schema.json"


def main() -> int:
    errors: list[str] = []
    schema_paths = sorted(SCHEMA_DIR.glob("*.schema.json"))
    validators: dict[Path, Draft202012Validator] = {}
    store = schema_store()

    for schema_path in schema_paths:
        if not VERSIONED_SCHEMA_NAME.fullmatch(schema_path.name):
            errors.append(f"schema {schema_path.relative_to(ROOT)}: filename must include a version such as .v1.schema.json")
            continue
        try:
            validators[schema_path] = validator_for(schema_path, store)
            validators[schema_path].check_schema(load_json(schema_path))
        except Exception as exc:  # pragma: no cover - command-line diagnostics
            errors.append(f"schema {schema_path.relative_to(ROOT)}: {exc}")

    for example_path in sorted(EXAMPLE_DIR.glob("*.json")):
        if not VERSIONED_EXAMPLE_NAME.fullmatch(example_path.name):
            errors.append(f"example {example_path.relative_to(ROOT)}: filename must include a version such as .v1.json")
            continue
        schema_path = positive_schema_for(example_path)
        validator = validators.get(schema_path)
        if validator is None:
            errors.append(f"example {example_path.relative_to(ROOT)}: schema not found")
            continue
        try:
            validator.validate(load_json(example_path))
        except Exception as exc:  # pragma: no cover - command-line diagnostics
            errors.append(f"example {example_path.relative_to(ROOT)}: {exc}")

    for example_path in sorted((EXAMPLE_DIR / "invalid").glob("*.json")):
        schema_path = invalid_schema_for(example_path)
        validator = validators.get(schema_path)
        if validator is None:
            errors.append(f"invalid example {example_path.relative_to(ROOT)}: schema not found")
            continue
        try:
            instance = load_json(example_path)
            if not list(validator.iter_errors(instance)):
                errors.append(f"invalid example {example_path.relative_to(ROOT)} unexpectedly passed")
        except Exception as exc:  # pragma: no cover - command-line diagnostics
            errors.append(f"invalid example {example_path.relative_to(ROOT)}: {exc}")

    if errors:
        print("Contract validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print(f"Validated {len(schema_paths)} schemas, {len(list(EXAMPLE_DIR.glob('*.json')))} valid examples, and {len(list((EXAMPLE_DIR / 'invalid').glob('*.json')))} invalid examples.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

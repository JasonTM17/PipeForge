from __future__ import annotations

import io
import json
from collections.abc import Callable
from uuid import uuid4

import polars as pl
import pytest

from pipeforge_worker.contracts.operations import (
    OperationConfigError,
    OperationType,
)
from pipeforge_worker.contracts.quality import parse_quality_rules
from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.protocols import ProcessingContext
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.readers.models import DataChunk, DatasetFormat, ReaderMetadata, ReaderOptions
from pipeforge_worker.validators.dispatch import quality_operation_dispatcher
from pipeforge_worker.validators.engine import QualityAggregator


def _rules() -> tuple:
    return parse_quality_rules(
        [
            {
                "id": "not-null",
                "name": "email is required",
                "columnScope": ["email"],
                "ruleType": "NOT_NULL",
                "configuration": {},
                "severity": "ERROR",
            },
            {
                "id": "unique-id",
                "name": "id is unique",
                "columnScope": ["id"],
                "ruleType": "UNIQUE",
                "configuration": {},
                "severity": "CRITICAL",
            },
            {
                "id": "score-range",
                "name": "score is bounded",
                "columnScope": ["score"],
                "ruleType": "BETWEEN",
                "configuration": {"min": 0, "max": 10},
                "severity": "WARNING",
            },
            {
                "id": "code-format",
                "name": "code is uppercase",
                "columnScope": ["code"],
                "ruleType": "REGEX",
                "configuration": {"pattern": "^[A-Z]+$"},
                "severity": "INFO",
            },
            {
                "id": "email-format",
                "name": "email has a domain",
                "columnScope": ["email"],
                "ruleType": "EMAIL_FORMAT",
                "configuration": {},
                "severity": "ERROR",
            },
            {
                "id": "date-format",
                "name": "date is ISO",
                "columnScope": ["created"],
                "ruleType": "DATE_FORMAT",
                "configuration": {"format": "%Y-%m-%d"},
                "severity": "ERROR",
            },
            {
                "id": "allowed",
                "name": "code is allowed",
                "columnScope": ["code"],
                "ruleType": "ALLOWED_VALUES",
                "configuration": {"values": ["A", "B"]},
                "severity": "WARNING",
            },
            {
                "id": "type",
                "name": "score is numeric",
                "columnScope": ["score"],
                "ruleType": "COLUMN_TYPE",
                "configuration": {"type": "number"},
                "severity": "ERROR",
            },
            {
                "id": "row-count",
                "name": "row count is bounded",
                "columnScope": [],
                "ruleType": "ROW_COUNT_BETWEEN",
                "configuration": {"min": 2, "max": 4},
                "severity": "INFO",
            },
            {
                "id": "reference",
                "name": "code is in reference set",
                "columnScope": ["code"],
                "ruleType": "REFERENTIAL_SET",
                "configuration": {"values": ["A", "B"]},
                "severity": "WARNING",
            },
        ]
    )


def _run(rules: tuple, frames: list[pl.DataFrame], clock: Callable[[], float]) -> dict[str, object]:
    aggregator = QualityAggregator(rules, monotonic=clock)
    aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("id", "email", "score", "code", "created")))
    for index, frame in enumerate(frames):
        aggregator.consume(DataChunk(index, index * 2, frame))
    return dict(aggregator.finish())


def test_quality_rules_aggregate_across_chunks_without_returning_raw_rows() -> None:
    report = _run(
        _rules(),
        [
            pl.DataFrame(
                {
                    "id": [1, 2],
                    "email": ["a@example.test", None],
                    "score": [5, -1],
                    "code": ["A", "bad"],
                    "created": ["2026-01-01", "2026-01-02"],
                },
                strict=False,
            ),
            pl.DataFrame(
                {
                    "id": [2, 3],
                    "email": ["b@example.test", "broken"],
                    "score": [20, "not-a-number"],
                    "code": ["B", "C"],
                    "created": ["not-a-date", "2026-01-04"],
                },
                strict=False,
            ),
        ],
        iter((0.0, 1.0)).__next__,
    )
    results = {item["id"]: item for item in report["rules"]}  # type: ignore[union-attr]
    assert results["not-null"]["failedRowCount"] == 1
    assert results["unique-id"]["failedRowCount"] == 1
    assert results["unique-id"]["status"] == "FAILED"
    assert results["score-range"]["failedRowCount"] == 3
    assert results["date-format"]["failedRowCount"] == 1
    assert results["row-count"]["status"] == "PASSED"
    assert results["reference"]["failedRowCount"] == 2
    assert all("raw" not in json.dumps(item) for item in results.values())
    assert all(
        set(reference) == {"rowNumber", "rowHash"}
        for item in results.values()
        for reference in item["failureReferences"]
    )


def test_quality_missing_column_and_disabled_rule_are_deterministic() -> None:
    rules = parse_quality_rules(
        [
            {
                "id": "missing",
                "name": "missing column",
                "columnScope": ["absent"],
                "ruleType": "NOT_NULL",
                "configuration": {},
                "severity": "ERROR",
            },
            {
                "id": "disabled",
                "name": "disabled rule",
                "columnScope": ["value"],
                "ruleType": "NOT_NULL",
                "configuration": {},
                "severity": "INFO",
                "enabled": False,
            },
        ]
    )
    report = _run(rules, [pl.DataFrame({"value": [1]})], iter((0.0, 1.0)).__next__)
    results = {item["id"]: item for item in report["rules"]}  # type: ignore[union-attr]
    assert results["missing"]["status"] == "ERROR"
    assert results["missing"]["errorCode"] == "MISSING_COLUMN"
    assert results["disabled"]["status"] == "SKIPPED"


def test_quality_contract_rejects_custom_code_and_unsafe_regex() -> None:
    with pytest.raises(OperationConfigError, match="CUSTOM_EXPRESSION"):
        parse_quality_rules(
            [
                {
                    "id": "custom",
                    "name": "unsafe",
                    "columnScope": ["value"],
                    "ruleType": "CUSTOM_EXPRESSION",
                    "configuration": {"expression": "__import__('os').system('whoami')"},
                }
            ]
        )
    with pytest.raises(OperationConfigError, match="unsafe"):
        parse_quality_rules(
            [
                {
                    "id": "regex",
                    "name": "grouped regex",
                    "columnScope": ["value"],
                    "ruleType": "REGEX",
                    "configuration": {"pattern": "(a+)+$"},
                }
            ]
        )


def test_quality_operations_run_through_pipeline() -> None:
    pipeline = ProcessingPipeline(DatasetReaderFactory(), quality_operation_dispatcher())
    result = pipeline.process(
        io.BytesIO(b"id,email\n1,a@example.test\n1,\n"),
        source_name="input.csv",
        content_type="text/csv",
        operations=[
            {
                "type": OperationType.CHECK_MISSING_VALUES.value,
                "config": {"columns": ["email"]},
            },
            {
                "type": OperationType.CHECK_DUPLICATES.value,
                "config": {"keyColumns": ["id"]},
            },
        ],
        options=ReaderOptions(has_header=True, chunk_size=1),
        context=ProcessingContext(uuid4(), uuid4(), uuid4(), uuid4()),
    )
    assert result.operation_results["0:CHECK_MISSING_VALUES"]["rules"][0]["failedRowCount"] == 1  # type: ignore[index]
    assert result.operation_results["1:CHECK_DUPLICATES"]["rules"][0]["failedRowCount"] == 1  # type: ignore[index]

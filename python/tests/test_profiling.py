from __future__ import annotations

import io
import json
from collections.abc import Callable
from pathlib import Path
from uuid import uuid4

import jsonschema
import polars as pl

from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.protocols import ProcessingContext
from pipeforge_worker.profilers.aggregator import ProfileAggregator
from pipeforge_worker.profilers.config import ProfileConfig
from pipeforge_worker.profilers.dispatch import profile_operation_dispatcher
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.readers.models import (
    DataChunk,
    DatasetFormat,
    ReaderMetadata,
    ReaderOptions,
    ReaderStatsSnapshot,
)
from pipeforge_worker.reports.profile import serialize_profile, upload_profile
from pipeforge_worker.storage.minio import StoredObject


class FakeStore:
    def __init__(self) -> None:
        self.calls: list[tuple[str, bytes, int, str]] = []

    def upload_stream(
        self, key: str, stream: io.BytesIO, size: int, content_type: str
    ) -> StoredObject:
        payload = stream.read()
        self.calls.append((key, payload, size, content_type))
        return StoredObject(key, "etag", size, content_type)


def _profile(split: list[pl.DataFrame], clock: Callable[[], float]) -> dict[str, object]:
    aggregator = ProfileAggregator(ProfileConfig.from_mapping({}), monotonic=clock)
    aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("id", "email", "score")))
    for index, frame in enumerate(split):
        aggregator.consume(DataChunk(index, index + 2, frame))
    return aggregator.finish_with_stats(ReaderStatsSnapshot(100, 4, 0, 0, 0, 100, ()))


def test_profile_is_deterministic_for_the_same_chunk_stream() -> None:
    rows = [
        {"id": 1, "email": "a@example.test", "score": 1.0},
        {"id": 1, "email": "a@example.test", "score": 1.0},
        {"id": 2, "email": None, "score": 3.0},
        {"id": 3, "email": "b@example.test", "score": 4.0},
    ]
    frame = pl.DataFrame(rows)

    first = _profile([frame], iter((0.0, 1.0)).__next__)
    second = _profile([frame], iter((0.0, 1.0)).__next__)

    assert first == second
    assert first["dataset"]["rowCount"] == 4  # type: ignore[index]
    assert first["dataset"]["duplicateRowCount"] == 1  # type: ignore[index]
    email = next(column for column in first["columns"] if column["name"] == "email")  # type: ignore[index]
    assert email["missingCount"] == 1
    assert email["commonValues"] == []
    assert "@example.test" not in serialize_profile(first).decode()


def test_profile_bounds_distinct_values_and_common_value_redaction() -> None:
    config = ProfileConfig.from_mapping(
        {
            "includeCommonValues": True,
            "maxCommonValues": 2,
            "maxDistinctValues": 128,
            "sensitiveColumns": ["secret"],
        }
    )
    aggregator = ProfileAggregator(config, monotonic=iter((0.0, 1.0)).__next__)
    aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("value", "secret")))
    aggregator.consume(
        DataChunk(
            0,
            2,
            pl.DataFrame(
                {
                    "value": list(range(300)),
                    "secret": ["private"] * 300,
                }
            ),
        )
    )
    result = aggregator.finish_with_stats(ReaderStatsSnapshot(1, 300, 0, 0, 0, 1, ()))
    value = next(column for column in result["columns"] if column["name"] == "value")  # type: ignore[index]
    secret = next(column for column in result["columns"] if column["name"] == "secret")  # type: ignore[index]
    assert value["distinctApproximate"] is True
    assert value["commonValues"]
    assert secret["commonValues"] == []
    assert any("approximate" in warning for warning in result["warnings"])  # type: ignore[operator]


def test_empty_profile_has_safe_zero_throughput() -> None:
    aggregator = ProfileAggregator(ProfileConfig(), monotonic=iter((4.0, 4.0)).__next__)
    aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("empty",)))
    result = aggregator.finish_with_stats(ReaderStatsSnapshot(0, 0, 0, 0, 0, 0, ()))
    assert result["dataset"]["rowCount"] == 0  # type: ignore[index]
    assert result["dataset"]["throughputRowsPerSecond"] == 0.0  # type: ignore[index]
    assert result["columns"][0]["inferredType"] == "unknown"  # type: ignore[index]


def test_profile_artifact_is_schema_valid_and_attempt_scoped() -> None:
    report = _profile(
        [pl.DataFrame({"id": [1, 2], "score": [1.0, 2.0]})],
        iter((0.0, 1.0)).__next__,
    )
    schema = json.loads(
        Path("contracts/json-schema/profile.report.v1.schema.json").read_text(encoding="utf-8")
    )
    jsonschema.validate(report, schema)

    store = FakeStore()
    object_ref = upload_profile(store, uuid4(), 3, report)
    assert object_ref.key.startswith("reports/")
    assert "/attempt-3/profile.json" in object_ref.key
    assert store.calls[0][3] == "application/json"
    assert json.loads(store.calls[0][1]) == report


def test_profile_dispatcher_runs_through_pipeline() -> None:
    pipeline = ProcessingPipeline(
        reader_factory=DatasetReaderFactory(),
        operation_dispatcher=profile_operation_dispatcher(monotonic=iter((0.0, 1.0)).__next__),
    )
    result = pipeline.process(
        io.BytesIO(b"id\n1\n2\n"),
        source_name="input.csv",
        content_type="text/csv",
        operations=[{"type": "PROFILE_DATASET", "config": {}}],
        options=ReaderOptions(has_header=True),
        context=ProcessingContext(uuid4(), uuid4(), uuid4(), uuid4()),
    )
    report = result.operation_results["0:PROFILE_DATASET"]
    assert report["dataset"]["fileSizeBytes"] > 0  # type: ignore[index]

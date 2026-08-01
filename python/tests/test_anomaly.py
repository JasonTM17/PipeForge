from __future__ import annotations

import io
from uuid import uuid4

import polars as pl
import pytest

from pipeforge_worker.anomaly.aggregator import AnomalyAggregator
from pipeforge_worker.anomaly.config import AnomalyConfig, AnomalyMethod
from pipeforge_worker.anomaly.detectors import detect
from pipeforge_worker.anomaly.dispatch import anomaly_operation_dispatcher
from pipeforge_worker.contracts.operations import (
    OperationConfigError,
    OperationType,
    parse_operations,
)
from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.protocols import ProcessingContext
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.readers.models import DataChunk, DatasetFormat, ReaderMetadata, ReaderOptions
from pipeforge_worker.reports.anomaly import serialize_anomaly


def test_reference_detectors_match_stable_fixtures() -> None:
    iqr = detect([1, 2, 3, 4, 5, 100], AnomalyMethod.IQR, 1.5, 3)
    assert iqr.indices == (5,)

    z_score = detect([0, 0, 0, 0, 0, 10], AnomalyMethod.Z_SCORE, 1.5, 3)
    assert z_score.indices == (5,)

    modified = detect([1, 2, 3, 4, 5, 6, 7, 100], AnomalyMethod.MODIFIED_Z_SCORE, 3.5, 3)
    assert modified.indices == (7,)


def test_anomaly_aggregator_is_partition_invariant_and_does_not_return_raw_values() -> None:
    config = AnomalyConfig.from_mapping(
        {"column": "amount", "method": "IQR", "threshold": 1.5, "minimumSampleSize": 3}
    )
    frames = [
        pl.DataFrame({"amount": [1, 2]}),
        pl.DataFrame({"amount": [3, 4]}),
        pl.DataFrame({"amount": [5, 100]}),
    ]

    def run(partition: list[pl.DataFrame]) -> dict[str, object]:
        clock = iter((0.0, 1.0)).__next__
        aggregator = AnomalyAggregator(config, monotonic=clock)
        aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("amount",)))
        row_start = 0
        for index, frame in enumerate(partition):
            aggregator.consume(DataChunk(index, row_start, frame))
            row_start += frame.height
        return dict(aggregator.finish())

    split_report = run(frames)
    combined_report = run([pl.concat(frames)])
    assert split_report["status"] == "ANOMALIES_FOUND"
    assert split_report["anomalyCount"] == combined_report["anomalyCount"] == 1
    assert split_report["anomalyReferences"] == combined_report["anomalyReferences"]
    assert all(
        set(reference) == {"rowNumber", "rowHash"}
        for reference in split_report["anomalyReferences"]  # type: ignore[union-attr]
    )


def test_anomaly_handles_insufficient_constant_and_null_values() -> None:
    constant_config = AnomalyConfig.from_mapping({"column": "value", "method": "Z_SCORE"})
    aggregator = AnomalyAggregator(constant_config, monotonic=iter((0.0, 1.0)).__next__)
    aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("value",)))
    aggregator.consume(DataChunk(0, 0, pl.DataFrame({"value": [4, 4, 4]})))
    constant = aggregator.finish()
    assert constant["status"] == "SKIPPED"
    assert constant["errorCode"] == "ZERO_VARIANCE"

    fail_config = AnomalyConfig.from_mapping(
        {"column": "value", "method": "IQR", "nullPolicy": "FAIL", "sampleOutputLimit": 1}
    )
    fail_aggregator = AnomalyAggregator(fail_config, monotonic=iter((0.0, 1.0)).__next__)
    fail_aggregator.start(ReaderMetadata(DatasetFormat.CSV, ("value",)))
    fail_aggregator.consume(
        DataChunk(0, 0, pl.DataFrame({"value": [1, None, "bad", 2]}, strict=False))
    )
    failed = fail_aggregator.finish()
    assert failed["anomalyCount"] == 2
    assert len(failed["anomalyReferences"]) == 1  # type: ignore[arg-type]
    assert "NON_NUMERIC_VALUES_SKIPPED" not in failed["warnings"]  # type: ignore[operator]


def test_anomaly_contract_rejects_unsafe_or_non_finite_configuration() -> None:
    with pytest.raises(OperationConfigError, match="unknown fields"):
        parse_operations(
            [
                {
                    "type": OperationType.DETECT_OUTLIERS,
                    "config": {"column": "x", "method": "IQR", "unknown": True},
                }
            ]
        )
    with pytest.raises(OperationConfigError, match="outside the supported bound"):
        parse_operations(
            [
                {
                    "type": OperationType.DETECT_OUTLIERS,
                    "config": {"column": "x", "method": "IQR", "threshold": float("nan")},
                }
            ]
        )
    with pytest.raises(OperationConfigError, match="invalid"):
        parse_operations(
            [{"type": OperationType.DETECT_OUTLIERS, "config": {"column": "x", "method": "MAD"}}]
        )


def test_anomaly_dispatcher_runs_through_processing_pipeline() -> None:
    pipeline = ProcessingPipeline(DatasetReaderFactory(), anomaly_operation_dispatcher())
    result = pipeline.process(
        io.BytesIO(b"amount\n1\n2\n3\n100\n"),
        source_name="input.csv",
        content_type="text/csv",
        operations=[
            {
                "type": OperationType.DETECT_OUTLIERS.value,
                "config": {"column": "amount", "method": "IQR", "minimumSampleSize": 3},
            }
        ],
        options=ReaderOptions(has_header=True, chunk_size=2),
        context=ProcessingContext(uuid4(), uuid4(), uuid4(), uuid4()),
    )
    report = result.operation_results["0:DETECT_OUTLIERS"]
    assert report["status"] == "ANOMALIES_FOUND"
    assert report["anomalyCount"] == 1


def test_anomaly_report_serialization_is_finite_and_stable() -> None:
    payload = serialize_anomaly({"schemaVersion": "anomaly.report.v1", "threshold": 1.5})
    assert payload == b'{"schemaVersion":"anomaly.report.v1","threshold":1.5}'

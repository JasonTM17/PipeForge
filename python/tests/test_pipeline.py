from __future__ import annotations

import io
from collections.abc import Mapping
from uuid import uuid4

import pytest

from pipeforge_worker.contracts.operations import OperationType
from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.protocols import (
    CancellationRequested,
    MappingOperationDispatcher,
    ProcessingContext,
)
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.readers.models import DataChunk, ReaderMetadata, ReaderOptions


class RecordingAggregator:
    def __init__(self) -> None:
        self.metadata: ReaderMetadata | None = None
        self.rows = 0
        self.chunks = 0

    def start(self, metadata: ReaderMetadata) -> None:
        self.metadata = metadata

    def consume(self, chunk: DataChunk) -> None:
        self.rows += chunk.frame.height
        self.chunks += 1

    def finish(self) -> Mapping[str, object]:
        return {"rows": self.rows, "chunks": self.chunks}


def _dispatcher() -> MappingOperationDispatcher:
    return MappingOperationDispatcher(
        {operation: lambda _request: RecordingAggregator() for operation in OperationType}
    )


def _context(progress: list[object], cancelled: list[bool] | None = None) -> ProcessingContext:
    return ProcessingContext(
        job_id=uuid4(),
        attempt_id=uuid4(),
        lease_id=uuid4(),
        worker_id=uuid4(),
        is_cancelled=(lambda: cancelled[0]) if cancelled is not None else (lambda: False),
        on_progress=progress.append,
        monotonic=lambda: 10.0,
    )


def test_pipeline_dispatches_operations_and_reports_final_progress() -> None:
    progress: list[object] = []
    pipeline = ProcessingPipeline(
        DatasetReaderFactory(), _dispatcher(), progress_interval_seconds=0.01
    )

    result = pipeline.process(
        io.BytesIO(b"id,value\n1,2\n3,4\n"),
        source_name="input.csv",
        content_type="text/csv",
        operations=[{"type": "PROFILE_DATASET", "config": {}}],
        options=ReaderOptions(chunk_size=1, has_header=True),
        context=_context(progress),
    )

    assert result.stats.rows_emitted == 2
    assert result.metadata.columns == ("id", "value")
    assert result.operation_results["0:PROFILE_DATASET"] == {"rows": 2, "chunks": 2}
    assert progress[-1].final is True


def test_pipeline_checks_cancellation_between_chunks() -> None:
    progress: list[object] = []
    cancelled = [True]
    pipeline = ProcessingPipeline(DatasetReaderFactory(), _dispatcher())

    with pytest.raises(CancellationRequested):
        pipeline.process(
            io.BytesIO(b"id\n1\n2\n"),
            source_name="input.csv",
            content_type="text/csv",
            operations=[{"type": "PROFILE_DATASET", "config": {}}],
            options=ReaderOptions(chunk_size=1, has_header=True),
            context=_context(progress, cancelled),
        )


def test_pipeline_rejects_unknown_operation_config() -> None:
    pipeline = ProcessingPipeline(DatasetReaderFactory(), _dispatcher())
    with pytest.raises(ValueError, match="unknown"):
        pipeline.process(
            io.BytesIO(b"id\n1\n"),
            source_name="input.csv",
            content_type="text/csv",
            operations=[{"type": "PROFILE_DATASET", "config": {"unsafe": True}}],
            options=ReaderOptions(has_header=True),
            context=_context([]),
        )

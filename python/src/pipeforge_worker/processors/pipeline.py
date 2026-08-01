"""Format-agnostic, cancellation-aware chunk processing pipeline."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from typing import Any, BinaryIO

from pipeforge_worker.contracts.operations import parse_operations
from pipeforge_worker.processors.protocols import (
    OperationDispatcher,
    ProcessingContext,
    ProcessingResult,
    ProgressReporter,
)
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.readers.models import ReaderOptions


class ProcessingPipeline:
    def __init__(
        self,
        reader_factory: DatasetReaderFactory,
        operation_dispatcher: OperationDispatcher,
        *,
        progress_interval_seconds: float = 1.0,
    ) -> None:
        self._reader_factory = reader_factory
        self._operation_dispatcher = operation_dispatcher
        self._progress_interval_seconds = progress_interval_seconds

    def process(
        self,
        source: BinaryIO,
        *,
        source_name: str | None,
        content_type: str | None,
        operations: Sequence[Mapping[str, Any]],
        options: ReaderOptions,
        context: ProcessingContext,
    ) -> ProcessingResult:
        parsed_operations = parse_operations(operations)
        aggregators = tuple(
            (operation, self._operation_dispatcher.build(operation))
            for operation in parsed_operations
        )
        with self._reader_factory.open(
            source,
            source_name=source_name,
            content_type=content_type,
            options=options,
        ) as session:
            for _, aggregator in aggregators:
                aggregator.start(session.metadata)
            reporter = ProgressReporter(context, self._progress_interval_seconds)
            for chunk in session.iter_chunks():
                context.check_cancellation()
                for _, aggregator in aggregators:
                    aggregator.consume(chunk)
                reporter.emit(session.stats.rows_emitted, session.metadata.estimated_rows)
            context.check_cancellation()
            reporter.emit(session.stats.rows_emitted, session.metadata.estimated_rows, final=True)
            results = {
                f"{index}:{operation.type.value}": dict(aggregator.finish())
                for index, (operation, aggregator) in enumerate(aggregators)
            }
            return ProcessingResult(
                metadata=session.metadata,
                stats=session.stats.snapshot(),
                operation_results=results,
            )

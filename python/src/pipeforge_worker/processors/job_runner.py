"""Adapt a fenced command into bounded processing and immutable report artifacts."""

from __future__ import annotations

import asyncio
import hashlib
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass
from io import BytesIO
from typing import Any, cast
from uuid import uuid4

from pipeforge_worker.consumers.job_execution_support import (
    ArtifactDescriptor,
    ProcessingRunResult,
)
from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.contracts.operations import (
    OperationRequest,
    OperationType,
    parse_operations,
)
from pipeforge_worker.messaging.protocols import PermanentMessageError
from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.protocols import (
    CancellationRequested,
    ProcessingContext,
)
from pipeforge_worker.readers.models import DatasetFormat, ReaderError, ReaderOptions
from pipeforge_worker.reports import serialize_anomaly, serialize_profile, serialize_quality
from pipeforge_worker.storage.minio import ObjectStore, artifact_key

MAX_SOURCE_BYTES = 8 * 1024 * 1024 * 1024
MAX_REPORT_BYTES = 16 * 1024 * 1024
DEFAULT_SNIFF_BYTES = 64 * 1024


@dataclass(frozen=True, slots=True)
class SourceDescriptor:
    object_key: str
    content_type: str
    filename: str
    size_bytes: int


class ProcessingJobRunner:
    """Run a command against the dataset bucket and upload immutable JSON artifacts."""

    def __init__(
        self,
        source_store: ObjectStore,
        artifact_store: ObjectStore,
        pipeline: ProcessingPipeline,
    ) -> None:
        self._source_store = source_store
        self._artifact_store = artifact_store
        self._pipeline = pipeline

    async def __call__(self, command: Envelope, context: ProcessingContext) -> ProcessingRunResult:
        source, operations = _parse_command(command)
        context.check_cancellation()
        try:
            with self._source_store.download_stream(source.object_key) as stream:
                result = await asyncio.to_thread(
                    self._pipeline.process,
                    stream,
                    source_name=source.filename,
                    content_type=source.content_type,
                    operations=_raw_operations(command),
                    options=ReaderOptions(max_bytes=max(source.size_bytes, DEFAULT_SNIFF_BYTES)),
                    context=context,
                )
        except CancellationRequested:
            raise
        except ReaderError as exc:
            raise PermanentMessageError("dataset input is invalid") from exc
        except PermanentMessageError:
            raise
        except Exception as exc:
            raise RuntimeError("dataset processing or source retrieval failed") from exc

        artifacts: list[ArtifactDescriptor] = []
        for index, operation in enumerate(operations):
            context.check_cancellation()
            result_key = f"{index}:{operation.type.value}"
            report = result.operation_results[result_key]
            artifacts.append(self._upload_report(context, operation.type, report))
        return ProcessingRunResult(tuple(artifacts))

    def _upload_report(
        self,
        context: ProcessingContext,
        operation_type: OperationType,
        report: Mapping[str, object],
    ) -> ArtifactDescriptor:
        kind, serialize = _artifact_serializer(operation_type)
        payload = serialize(report)
        if len(payload) > MAX_REPORT_BYTES:
            raise RuntimeError("generated report exceeds the artifact size limit")
        artifact_id = uuid4()
        key = artifact_key(
            context.job_id,
            _attempt_number(context),
            f"{artifact_id}-{kind}.json",
        )
        stored = self._artifact_store.upload_stream(
            key,
            BytesIO(payload),
            len(payload),
            "application/json",
        )
        return ArtifactDescriptor(
            artifact_id=artifact_id,
            kind=kind,
            object_key=stored.key,
            size_bytes=stored.size,
            content_type=stored.content_type,
            checksum_sha256=hashlib.sha256(payload).hexdigest(),
        )


def _parse_command(command: Envelope) -> tuple[SourceDescriptor, tuple[OperationRequest, ...]]:
    payload = command.payload
    source = payload.get("source")
    if not isinstance(source, Mapping):
        raise PermanentMessageError("job command source is invalid")
    return _parse_source(source), _parse_operations(_raw_operations(command))


def _parse_source(source: Mapping[str, object]) -> SourceDescriptor:
    object_key = source.get("objectKey")
    content_type = source.get("contentType")
    format_value = source.get("format")
    size_bytes = source.get("sizeBytes")
    if (
        not isinstance(object_key, str)
        or not object_key.startswith("datasets/")
        or ".." in object_key
        or "\\" in object_key
        or not isinstance(content_type, str)
        or not content_type.strip()
        or len(content_type) > 255
        or isinstance(size_bytes, bool)
        or not isinstance(size_bytes, int)
        or not 1 <= size_bytes <= MAX_SOURCE_BYTES
    ):
        raise PermanentMessageError("job command source is invalid")
    try:
        dataset_format = DatasetFormat(str(format_value))
    except ValueError as exc:
        raise PermanentMessageError("job command source format is invalid") from exc
    extension = {
        DatasetFormat.CSV: "csv",
        DatasetFormat.JSONL: "jsonl",
        DatasetFormat.PARQUET: "parquet",
    }
    return SourceDescriptor(
        object_key, content_type.strip(), f"source.{extension[dataset_format]}", size_bytes
    )


def _raw_operations(command: Envelope) -> list[Mapping[str, Any]]:
    operations = command.payload.get("operations")
    if not isinstance(operations, list) or not all(
        isinstance(item, Mapping) for item in operations
    ):
        raise PermanentMessageError("job command operations are invalid")
    return cast(list[Mapping[str, Any]], operations)


def _parse_operations(operations: Sequence[Mapping[str, Any]]) -> tuple[OperationRequest, ...]:
    try:
        return parse_operations(operations)
    except (TypeError, ValueError) as exc:
        raise PermanentMessageError("job command operations are invalid") from exc


ReportSerializer = Callable[[Mapping[str, object]], bytes]


def _artifact_serializer(operation_type: OperationType) -> tuple[str, ReportSerializer]:
    if operation_type is OperationType.PROFILE_DATASET:
        return "profile", serialize_profile
    if operation_type in {
        OperationType.CHECK_MISSING_VALUES,
        OperationType.CHECK_DUPLICATES,
        OperationType.VALIDATE_QUALITY,
    }:
        return "quality", serialize_quality
    if operation_type is OperationType.DETECT_OUTLIERS:
        return "anomaly", serialize_anomaly
    raise ValueError(f"no artifact serializer registered for {operation_type.value}")


def _attempt_number(context: ProcessingContext) -> int:
    if context.attempt_number < 1:
        raise ValueError("attempt number must be positive")
    return context.attempt_number

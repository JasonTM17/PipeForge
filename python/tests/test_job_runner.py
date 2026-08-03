from __future__ import annotations

import hashlib
import json
from collections.abc import Iterator
from contextlib import contextmanager
from io import BytesIO
from uuid import UUID, uuid4

import pytest

from pipeforge_worker.consumers.job_execution_support import ProcessingRunResult
from pipeforge_worker.contracts.envelope import Envelope, build_envelope
from pipeforge_worker.messaging.protocols import PermanentMessageError
from pipeforge_worker.processors.job_runner import ProcessingJobRunner
from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.production_operation_dispatcher import (
    ProductionOperationDispatcher,
)
from pipeforge_worker.processors.protocols import ProcessingContext
from pipeforge_worker.readers.factory import DatasetReaderFactory
from pipeforge_worker.storage.minio import StoredObject


class FakeStore:
    def __init__(self, source: bytes = b"", fail_upload: bool = False) -> None:
        self._source = source
        self._fail_upload = fail_upload
        self.downloaded_keys: list[str] = []
        self.uploads: list[tuple[str, bytes, int, str]] = []

    @contextmanager
    def download_stream(self, key: str) -> Iterator[BytesIO]:
        self.downloaded_keys.append(key)
        yield BytesIO(self._source)

    def upload_stream(
        self, key: str, stream: BytesIO, size: int, content_type: str
    ) -> StoredObject:
        if self._fail_upload:
            raise OSError("artifact bucket is unavailable")
        payload = stream.read()
        self.uploads.append((key, payload, size, content_type))
        return StoredObject(key, "etag", size, content_type)

    def close(self) -> None:
        return


def _command(*, object_key: str = "datasets/fixture/versions/fixture/raw") -> Envelope:
    job_id = uuid4()
    return build_envelope(
        "processing.job.requested",
        "trace",
        str(job_id),
        "request",
        {
            "jobId": str(job_id),
            "datasetVersionId": str(uuid4()),
            "attemptId": str(uuid4()),
            "leaseId": str(uuid4()),
            "workerId": str(uuid4()),
            "attemptNumber": 4,
            "source": {
                "objectKey": object_key,
                "contentType": "text/csv",
                "format": "CSV",
                "sizeBytes": 64,
            },
            "operations": [
                {"type": "PROFILE_DATASET", "config": {}},
                {"type": "CHECK_MISSING_VALUES", "config": {"columns": ["email"]}},
                {
                    "type": "DETECT_OUTLIERS",
                    "config": {
                        "column": "amount",
                        "method": "IQR",
                        "minimumSampleSize": 3,
                    },
                },
            ],
        },
    )


def _context(command: Envelope) -> ProcessingContext:
    payload = command.payload
    return ProcessingContext(
        job_id=UUID(str(payload["jobId"])),
        attempt_id=UUID(str(payload["attemptId"])),
        lease_id=UUID(str(payload["leaseId"])),
        worker_id=UUID(str(payload["workerId"])),
        attempt_number=4,
    )


@pytest.mark.asyncio
async def test_runner_processes_source_and_writes_immutable_attempt_scoped_reports() -> None:
    command = _command()
    source = FakeStore(
        b"id,email,amount\n1,a@example.test,1\n2,,2\n3,b@example.test,3\n4,c@example.test,100\n"
    )
    artifacts = FakeStore()
    runner = ProcessingJobRunner(
        source,
        artifacts,
        ProcessingPipeline(DatasetReaderFactory(), ProductionOperationDispatcher()),
    )

    result = await runner(command, _context(command))

    assert isinstance(result, ProcessingRunResult)
    assert source.downloaded_keys == [command.payload["source"]["objectKey"]]
    assert [artifact.kind for artifact in result.artifacts] == ["profile", "quality", "anomaly"]
    assert len({artifact.object_key for artifact in result.artifacts}) == 3
    assert all(
        f"reports/{command.payload['jobId']}/attempt-4/" in item.object_key
        for item in result.artifacts
    )
    for artifact, (_, payload, size, content_type) in zip(
        result.artifacts, artifacts.uploads, strict=True
    ):
        assert artifact.size_bytes == size == len(payload)
        assert artifact.content_type == content_type == "application/json"
        assert artifact.checksum_sha256 == hashlib.sha256(payload).hexdigest()
    assert json.loads(artifacts.uploads[0][1])["schemaVersion"] == 1
    assert json.loads(artifacts.uploads[1][1])["schemaVersion"] == "quality.report.v1"
    assert json.loads(artifacts.uploads[2][1])["schemaVersion"] == "anomaly.report.v1"


@pytest.mark.asyncio
async def test_runner_rejects_untrusted_source_before_download() -> None:
    command = _command(object_key="reports/other-job/attempt-1/profile.json")
    source = FakeStore()
    runner = ProcessingJobRunner(
        source,
        FakeStore(),
        ProcessingPipeline(DatasetReaderFactory(), ProductionOperationDispatcher()),
    )

    with pytest.raises(PermanentMessageError, match="source is invalid"):
        await runner(command, _context(command))

    assert source.downloaded_keys == []


@pytest.mark.asyncio
async def test_runner_surfaces_artifact_upload_failure_for_retryable_execution_failure() -> None:
    command = _command()
    source = FakeStore(
        b"id,email,amount\n1,a@example.test,1\n2,,2\n3,b@example.test,3\n4,c@example.test,100\n"
    )
    runner = ProcessingJobRunner(
        source,
        FakeStore(fail_upload=True),
        ProcessingPipeline(DatasetReaderFactory(), ProductionOperationDispatcher()),
    )

    with pytest.raises(OSError, match="artifact bucket"):
        await runner(command, _context(command))

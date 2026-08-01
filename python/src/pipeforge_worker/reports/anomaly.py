"""Stable anomaly-report serialization and attempt-scoped artifact upload."""

from __future__ import annotations

import json
from collections.abc import Mapping
from io import BytesIO
from uuid import UUID

from pipeforge_worker.storage.minio import ObjectStore, StoredObject, artifact_key


def serialize_anomaly(report: Mapping[str, object]) -> bytes:
    return json.dumps(
        dict(report), ensure_ascii=True, sort_keys=True, separators=(",", ":"), allow_nan=False
    ).encode("utf-8")


def upload_anomaly(
    store: ObjectStore,
    job_id: UUID,
    attempt_number: int,
    report: Mapping[str, object],
) -> StoredObject:
    payload = serialize_anomaly(report)
    if len(payload) > 16 * 1024 * 1024:
        raise ValueError("anomaly report exceeds the artifact size limit")
    return store.upload_stream(
        artifact_key(job_id, attempt_number, "anomaly.json"),
        BytesIO(payload),
        len(payload),
        "application/json",
    )

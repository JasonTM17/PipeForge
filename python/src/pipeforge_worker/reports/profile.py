"""Stable JSON profile serialization and attempt-scoped upload."""

from __future__ import annotations

import json
from collections.abc import Mapping
from io import BytesIO
from uuid import UUID

from pipeforge_worker.storage.minio import ObjectStore, StoredObject, artifact_key


def serialize_profile(report: Mapping[str, object]) -> bytes:
    """Serialize a profile with stable key order and JSON-safe finite numbers."""

    encoded = json.dumps(
        dict(report),
        ensure_ascii=True,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    )
    return encoded.encode("utf-8")


def upload_profile(
    store: ObjectStore,
    job_id: UUID,
    attempt_number: int,
    report: Mapping[str, object],
) -> StoredObject:
    payload = serialize_profile(report)
    if len(payload) > 16 * 1024 * 1024:
        raise ValueError("profile report exceeds the artifact size limit")
    key = artifact_key(job_id, attempt_number, "profile.json")
    return store.upload_stream(
        key,
        BytesIO(payload),
        len(payload),
        "application/json",
    )

from uuid import uuid4

import pytest

from pipeforge_worker.storage.minio import artifact_key, quarantine_key


def test_attempt_scoped_keys_are_safe_and_deterministic() -> None:
    job_id = uuid4()
    assert artifact_key(job_id, 2, "report.json") == f"reports/{job_id}/attempt-2/report.json"
    assert quarantine_key(job_id, 1, "rows.ndjson") == f"quarantine/{job_id}/attempt-1/rows.ndjson"


@pytest.mark.parametrize("filename", ["../rows.json", "folder/rows.json", "", "rows secret.json"])
def test_attempt_scoped_keys_reject_path_traversal(filename: str) -> None:
    with pytest.raises(ValueError):
        artifact_key(uuid4(), 1, filename)

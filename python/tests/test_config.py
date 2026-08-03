from pathlib import Path
from uuid import UUID

import pytest
from pydantic import ValidationError

from pipeforge_worker.config import Settings, redacted_settings


def test_settings_parse_bounded_operations_and_redacted_projection(tmp_path: Path) -> None:
    worker_id = UUID("AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE")
    settings = Settings(
        contracts_dir=tmp_path,
        worker_id=worker_id,
        supported_operations="PROFILE_DATASET,CHECK_MISSING_VALUES",
        minio_secret_key="not-logged",
    )

    assert settings.supported_operations == ("PROFILE_DATASET", "CHECK_MISSING_VALUES")
    assert settings.job_queue_name == "processing.jobs.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    assert settings.cancellation_queue_name == (
        "processing.cancellations.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    )
    assert settings.job_routing_key == (
        "processing.job.requested.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    )
    assert settings.cancellation_routing_key == (
        "processing.job.cancel-requested.aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
    )
    assert redacted_settings(settings).model_dump()["broker_queue"] == settings.job_queue_name
    assert settings.legacy_shared_queue_compatibility is False
    assert "minio_secret_key" not in redacted_settings(settings).model_dump()


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("max_concurrency", 0),
        ("prefetch_count", 0),
        ("health_port", 70000),
        ("heartbeat_interval_seconds", 0.5),
    ],
)
def test_settings_reject_invalid_bounds(tmp_path: Path, field: str, value: object) -> None:
    with pytest.raises(ValidationError):
        Settings(contracts_dir=tmp_path, **{field: value})


def test_health_only_allows_missing_contract_directory(tmp_path: Path) -> None:
    settings = Settings(contracts_dir=tmp_path / "missing", health_only=True)
    assert settings.health_only is True

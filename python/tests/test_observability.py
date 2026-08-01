import json
import logging

from prometheus_client import CollectorRegistry

from pipeforge_worker.observability.health import HealthState
from pipeforge_worker.observability.logging import JsonFormatter, redact_fields
from pipeforge_worker.observability.metrics import WorkerMetrics


def test_redaction_hides_credentials_and_presigned_urls() -> None:
    fields = redact_fields(
        {
            "password": "secret-value",
            "authorization": "Bearer abc",
            "url": "https://user:password@example.test/file?X-Amz-Signature=abc",
            "rows": [{"email": "private@example.test"}],
        }
    )

    assert fields["password"] == "[REDACTED]"
    assert "abc" not in str(fields["authorization"])
    assert "password@example" not in str(fields["url"])
    assert fields["rows"][0]["email"] == "private@example.test"


def test_json_formatter_emits_safe_structured_record() -> None:
    record = logging.LogRecord("test", logging.INFO, __file__, 1, "connected", (), None)
    record.pipeforge_fields = {"event": "connected", "secret": "hidden"}  # type: ignore[attr-defined]
    payload = json.loads(JsonFormatter().format(record))
    assert payload["event"] == "connected"
    assert payload["secret"] == "[REDACTED]"


def test_health_state_caps_job_visibility_and_tracks_readiness() -> None:
    state = HealthState(max_concurrency=2)
    metrics = WorkerMetrics(CollectorRegistry())
    metrics.set_status("READY")
    state.set_status("READY")
    state.set_dependency("broker", True)
    state.set_current_jobs([str(index) for index in range(200)])

    snapshot = state.snapshot()
    assert state.is_ready() is True
    assert snapshot["currentConcurrency"] == 128
    assert snapshot["maxConcurrency"] == 2

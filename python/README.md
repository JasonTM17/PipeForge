# PipeForge worker

The Python worker is the data-plane process for PipeForge. Phase 9 provides the
typed runtime foundation: bounded settings, structured redacted logs,
Prometheus metrics, health endpoints, validated message envelopes, RabbitMQ
manual acknowledgements, MinIO streaming adapters, worker registration, and
heartbeat publication.

## Local checks

From the repository root:

```powershell
python -m venv python/.venv
python/.venv/Scripts/python.exe -m pip install -e "python[dev]"
python/.venv/Scripts/python.exe -m pytest -q
python/.venv/Scripts/ruff.exe check python/src python/tests
python/.venv/Scripts/ruff.exe format --check python/src python/tests
python/.venv/Scripts/python.exe -m mypy python/src
```

Set `PIPEFORGE_WORKER_HEALTH_ONLY=true` to start only the health/metrics
server while the processing pipeline is not enabled. Dataset readers and
operation execution are added in the next phases.

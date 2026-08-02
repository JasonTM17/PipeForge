# PipeForge worker

The Python worker is the data-plane process for PipeForge. The foundation
provides bounded settings, structured redacted logs, Prometheus metrics, health
endpoints, validated message envelopes, RabbitMQ manual acknowledgements,
MinIO streaming adapters, worker registration, and heartbeat publication.
Dataset readers now detect and stream CSV, JSON Lines, and Parquet into bounded
Polars frames. The injected processing pipeline adds operation dispatch,
progress callbacks, and cooperative cancellation; profiling, quality, anomaly,
and result coordination remain separate phases.

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
server. The cancellation-aware executor and fencing token registry are covered
by worker tests, but the default RabbitMQ command classifier remains
intentionally fail-closed until Phase 20 connects lease-bound source
resolution, artifact publication, and result events; direct pipeline
composition is covered by the reader/pipeline tests.

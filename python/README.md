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
server. The RabbitMQ command classifier remains intentionally fail-closed until
lease-bound source resolution and result publication are available; direct
pipeline composition is covered by the reader/pipeline tests.

# Data-plane boundary

The Python data plane performs computation only. A worker receives a validated command, downloads the referenced immutable dataset object, processes bounded chunks, writes attempt-scoped artifacts, and publishes an event. It does not become the source of truth for job state.

## Phase 9 worker foundation

The package lives under `python/src/pipeforge_worker` and keeps external clients behind injected protocols. `Settings` validates bounded concurrency, prefetch, message size, heartbeat, and shutdown limits. The worker exposes `/health/live`, `/health/ready`, and `/metrics` on port `8090`; `PIPEFORGE_WORKER_HEALTH_ONLY=true` starts this surface without broker or object-storage intake.

The RabbitMQ adapter uses durable queues, persistent messages, publisher confirms, bounded prefetch, and manual acknowledgement. A command is acknowledged only after its handler returns `ACK`; malformed or not-yet-supported commands are rejected with `requeue=false`, which hands them to the configured dead-letter exchange. The phase-9 handler intentionally classifies valid jobs to DLQ until the processing pipeline is implemented.

Worker registration and heartbeat messages are validated against the shared JSON Schemas. Heartbeats expose only bounded worker metadata and at most 128 opaque job IDs. Structured logs redact credentials, bearer tokens, and presigned URL credentials; raw dataset rows are never logged.

## Processing pipeline

```mermaid
flowchart LR
    Command["Validated job command"] --> Detect["Detect format"]
    Detect --> Reader["CSV / JSONL / Parquet reader"]
    Reader --> Chunks["Bounded chunk iterator"]
    Chunks --> Profile["Profile aggregators"]
    Chunks --> Quality["Quality validators"]
    Chunks --> Anomaly["Anomaly detectors"]
    Profile --> Report["Versioned report builder"]
    Quality --> Report
    Anomaly --> Report
    Report --> Upload["Attempt-scoped MinIO artifacts"]
    Upload --> Event["Versioned result event"]
```

## Design rules

- Reader, processor, validator, detector, storage, and publisher interfaces are dependency-injected and testable with fakes.
- Polars is the initial dataframe engine because it supports columnar execution and bounded streaming patterns; the adapter boundary leaves room for another engine without changing contracts.
- Bad-row policies are explicit: fail fast, skip/report, or quarantine. Quarantine references are bounded and never log complete sensitive rows.
- Progress is throttled by time or percentage change. One message per row is prohibited.
- Cancellation is checked between chunks/stages. A worker stops safely, cleans temporary artifacts, and publishes a cancellation outcome.
- Generated artifacts use `reports/{jobId}/attempt-{attemptNumber}/...`-style keys. The worker never overwrites a canonical successful artifact.

## Failure behavior

Format/configuration/missing-column failures are non-retryable. Temporary broker/storage/network/resource failures are retryable within the Go-owned bounded policy. Lease renewal failure stops the worker from publishing an authoritative success for stale work; late events are ignored by the Go result consumer.

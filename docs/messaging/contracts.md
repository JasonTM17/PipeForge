# Messaging contracts

PipeForge messages use JSON envelopes defined under [`contracts/json-schema`](../../contracts/json-schema/). Every message carries a UUID, stable type, schema version, event time, and trace/correlation/causation identifiers. Payloads contain references and bounded metadata; raw dataset rows and credentials never cross the broker.

## Validation

Install the small validation dependency once, then run the shared fixture validator from the repository root:

```powershell
python -m pip install -r scripts/requirements.txt
python scripts/validate-contracts.py
```

Go publishers reject unknown message types, missing envelope fields, invalid payload objects, oversized messages, and missing type-specific required fields before contacting RabbitMQ. Python consumers use the same schemas and must reject invalid messages before processing.

The `processing.job.requested` payload accepts the operation types implemented by the control plane: `PROFILE_DATASET`, `CHECK_MISSING_VALUES`, `CHECK_DUPLICATES`, `VALIDATE_QUALITY`, and `DETECT_OUTLIERS`. Column identifiers are bounded SQL-style names (`[A-Za-z_][A-Za-z0-9_]{0,127}`); missing-value and duplicate checks require at least one column, while outlier detection supports `IQR`, `Z_SCORE`, and `MODIFIED_Z_SCORE` with bounded threshold, null-policy, minimum-sample, and reference-limit options. Profile configuration is optional but bounded: deterministic reservoir sampling, quantiles, exact/approximate distinct strategy, common-value limits, memory budget, and sensitive-column suppression are validated before dispatch. Quality snapshots contain 1–256 strict rule definitions; `CUSTOM_EXPRESSION` is rejected at both the Go and Python boundaries. Unknown configuration keys are rejected by the shared schema.

## Message catalogue

| Type | Direction | Exchange/routing key | Purpose |
| --- | --- | --- | --- |
| `processing.job.requested` | Go → worker | `pipeforge.commands` / same key | Start an idempotent job attempt. |
| `processing.job.cancel-requested` | Go → worker | `pipeforge.commands` / same key | Ask a worker to stop between bounded stages. |
| `processing.job.started` | worker → Go | `pipeforge.events` / same key | Report the validated attempt and lease. |
| `processing.job.progressed` | worker → Go | `pipeforge.events` / same key | Publish throttled progress snapshots. |
| `processing.job.succeeded` | worker → Go | `pipeforge.events` / same key | Publish accepted attempt artifact references. |
| `processing.job.failed` | worker → Go | `pipeforge.events` / same key | Publish classified retryable/permanent failure. |
| `processing.worker.registered` | worker → Go | `pipeforge.events` / same key | Register worker capabilities. |
| `processing.worker.heartbeat` | worker → Go | `pipeforge.events` / same key | Refresh worker liveness and capacity. |
| `processing.artifact.created` | worker → Go | `pipeforge.events` / same key | Announce an attempt-scoped object after upload. |

## Compatibility rules

- Additive optional fields are allowed within a schema version.
- Removing, renaming, or changing the meaning/type of a required field requires a new `schemaVersion` and a new message type migration path.
- Producers publish only after local validation and with persistent delivery mode.
- Consumers acknowledge only after the durable outcome is committed; malformed messages are rejected to a bounded DLQ path rather than endlessly requeued.

The Python worker follows the same boundary: it validates the complete envelope and payload before dispatch, acknowledges only after the injected handler completes, and rejects malformed, unsupported, or failed commands with `requeue=false`. This makes broker dead-lettering the durable classification until the Go-owned lease/result workflow is available.

# Messaging contracts

PipeForge messages use JSON envelopes defined under [`contracts/json-schema`](../../contracts/json-schema/). Every message carries a UUID, stable type, schema version, event time, and trace/correlation/causation identifiers. Payloads contain references and bounded metadata; raw dataset rows and credentials never cross the broker.

## Validation

Install the small validation dependency once, then run the shared fixture validator from the repository root:

```powershell
python -m pip install -r scripts/requirements.txt
python scripts/validate-contracts.py
```

Go publishers reject unknown message types, missing envelope fields, invalid payload objects, oversized messages, and missing type-specific required fields before contacting RabbitMQ. Python consumers use the same schemas and must reject invalid messages before processing.

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

# Phase 2: Worker-targeted command routing

## Files

- Modify Go queue routing helpers, lease command creation, cancellation target
  lookup, outbox routing, topology tests, and job/lease tests.
- Correct retry requests to emit `processing.job.queued`; they must never emit a
  worker command before the next lease exists.
- Modify Python worker queue naming, RabbitMQ binding support, app assembly, and
  tests.
- Modify Compose and `.env.example` to use worker-owned identities rather than a
  scheduler dispatch identity; add a two-worker local verification profile when
  it can stay deterministic and non-duplicative.

## Invariants

- Envelope `messageType` and payload schema remain version 1.
- Routing key suffix is the canonical lowercase worker UUID.
- Each worker declares durable job/cancellation queues bound only to its keys.
- Shared DLQ routing remains bounded and existing single-worker startup remains
  compatible.
- During cutover, only the designated legacy worker may consume the old shared
  queues, behind an explicit development migration flag. Secondary/new workers
  consume targeted queues only. Operators must drain legacy queues before
  disabling compatibility or enabling unrestricted multi-worker rollout.
- On graceful shutdown the active instance publishes forced DRAINING before
  intake stops and OFFLINE before the publisher closes. TTL remains the fallback
  when broker failure prevents terminal publication.

## Validation

- Go tests assert exact routing keys for acquire and cancel.
- Go tests assert retry events return to scheduler and never bypass leasing.
- Python tests assert deterministic queue names/bindings and wrong-worker
  commands cannot reach the consumer queue, plus terminal heartbeat ordering.
- Compose configuration validates with one and two worker services.

#!/usr/bin/env sh
set -eu

attempt=0
until rabbitmqadmin \
  --host rabbitmq \
  --port 15672 \
  --username "$RABBITMQ_DEFAULT_USER" \
  --password "$RABBITMQ_DEFAULT_PASS" \
  --vhost / \
  list vhosts >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo 'RabbitMQ management API did not become ready within 60 seconds.' >&2
    exit 1
  fi
  sleep 2
done

rabbitmqadmin \
    --host rabbitmq \
    --port 15672 \
    --username "$RABBITMQ_DEFAULT_USER" \
    --password "$RABBITMQ_DEFAULT_PASS" \
    --vhost / \
    import /opt/pipeforge/definitions.json

# Definitions import is additive. Remove the superseded wildcard binding so an
# existing local broker cannot route heartbeat and registration events to the
# result consumer.
for obsolete_key in 'processing.job.#' 'processing.#'; do
  rabbitmqadmin \
      --host rabbitmq \
      --port 15672 \
      --username "$RABBITMQ_DEFAULT_USER" \
      --password "$RABBITMQ_DEFAULT_PASS" \
      --vhost / \
      delete binding source=pipeforge.events destination_type=queue destination=control-plane.results properties_key="$obsolete_key" \
      >/dev/null 2>&1 || true
done

echo 'RabbitMQ topology imported.'

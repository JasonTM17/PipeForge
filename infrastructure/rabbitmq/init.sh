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

echo 'RabbitMQ topology imported.'

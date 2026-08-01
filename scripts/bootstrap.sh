#!/usr/bin/env sh
set -eu

if [ ! -f .env ]; then
  cp .env.example .env
  echo 'Created .env from .env.example; review local development values.'
fi

./scripts/validate-env.sh .env
docker compose up -d postgres rabbitmq rabbitmq-init minio minio-init
docker compose ps

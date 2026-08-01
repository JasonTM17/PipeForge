#!/usr/bin/env sh
set -eu

file="${1:-.env}"
if [ ! -f "$file" ]; then
  echo "Environment file not found: $file. Copy .env.example to .env first." >&2
  exit 1
fi

required="PIPEFORGE_ENV POSTGRES_DATABASE POSTGRES_USER POSTGRES_PASSWORD RABBITMQ_USER RABBITMQ_PASSWORD MINIO_ACCESS_KEY MINIO_SECRET_KEY"
for key in $required; do
  value=$(sed -n "s/^${key}=//p" "$file" | tail -n 1)
  if [ -z "$value" ]; then
    echo "Missing required environment value: $key" >&2
    exit 1
  fi
done

echo "Environment validated: $file"


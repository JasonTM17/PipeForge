#!/usr/bin/env sh
set -eu

mc alias set pipeforge http://minio:9000 "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY"
mc mb --ignore-existing pipeforge/datasets
mc mb --ignore-existing pipeforge/artifacts
mc mb --ignore-existing pipeforge/quarantine
mc anonymous set none pipeforge/datasets
mc anonymous set none pipeforge/artifacts
mc anonymous set none pipeforge/quarantine


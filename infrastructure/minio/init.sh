#!/usr/bin/env sh
set -eu

mc alias set pipeforge http://minio:9000 "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY"
dataset_bucket=${MINIO_DATASET_BUCKET:-datasets}
mc mb --ignore-existing "pipeforge/$dataset_bucket"
mc mb --ignore-existing pipeforge/artifacts
mc mb --ignore-existing pipeforge/quarantine
mc anonymous set none "pipeforge/$dataset_bucket"
mc anonymous set none pipeforge/artifacts
mc anonymous set none pipeforge/quarantine

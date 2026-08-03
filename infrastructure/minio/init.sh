#!/usr/bin/env sh
set -eu

mc alias set pipeforge http://minio:9000 "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY"
dataset_bucket=${MINIO_DATASET_BUCKET:-datasets}
artifact_bucket=${MINIO_ARTIFACT_BUCKET:-artifacts}
mc mb --ignore-existing "pipeforge/$dataset_bucket"
mc mb --ignore-existing "pipeforge/$artifact_bucket"
mc mb --ignore-existing pipeforge/quarantine
mc anonymous set none "pipeforge/$dataset_bucket"
mc anonymous set none "pipeforge/$artifact_bucket"
mc anonymous set none pipeforge/quarantine

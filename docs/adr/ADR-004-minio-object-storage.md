# ADR-004: MinIO object storage

## Status

Accepted

## Context

Input files and reports can be large and must not be loaded into API memory or stored in PostgreSQL.

## Decision

Use MinIO's S3-compatible object API for local raw dataset versions, multipart uploads, and attempt-scoped artifacts. Go generates safe opaque keys and Python uses injected storage adapters.

## Consequences

Local behavior is close to an S3 deployment and supports streaming. Object lifecycle, integrity checks, cleanup, and authorization must be handled explicitly in the control plane.

## Alternatives considered

- Filesystem volumes: simpler local setup, weaker object/presigned/multipart story and portability.
- PostgreSQL large objects: transactional but unsuitable for the intended large-file/data-plane demonstration.


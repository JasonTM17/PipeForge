# Container publication

PipeForge publishes five role-specific images from a single reviewed source
revision. The images are prerelease learning artifacts, not a production
deployment or support commitment.

## Registries and images

| Role | GHCR | Docker Hub |
|---|---|---|
| API | `ghcr.io/jasontm17/pipeforge-api` | `nguyenson1710/pipeforge-api` |
| Scheduler | `ghcr.io/jasontm17/pipeforge-scheduler` | `nguyenson1710/pipeforge-scheduler` |
| Result consumer | `ghcr.io/jasontm17/pipeforge-result-consumer` | `nguyenson1710/pipeforge-result-consumer` |
| Migration | `ghcr.io/jasontm17/pipeforge-migrate` | `nguyenson1710/pipeforge-migrate` |
| Python worker | `ghcr.io/jasontm17/pipeforge-worker` | `nguyenson1710/pipeforge-worker` |

Each publication writes the same two tags to both registries:

- the release version, such as `0.2.2-local`;
- `sha-<full-40-character-source-commit>` for source traceability.

The prerelease intentionally has no `latest` tag.

## Pull and verify

Pull a convenient version tag for evaluation:

```text
docker pull ghcr.io/jasontm17/pipeforge-api:0.2.2-local
docker pull nguyenson1710/pipeforge-api:0.2.2-local
```

For repeatable automation, copy a digest from the successful workflow's
`container-digest-*` artifacts and pin it:

```text
docker pull ghcr.io/jasontm17/pipeforge-api@sha256:<digest>
```

Verify GitHub build provenance against this repository:

```text
gh attestation verify oci://ghcr.io/jasontm17/pipeforge-api:0.2.2-local -R JasonTM17/PipeForge
```

Public GHCR images and public Docker Hub images can be pulled anonymously.

## Release gate

`.github/workflows/container-release.yml` runs only for a published GitHub
release or an explicit manual dispatch. For every role it:

1. checks out the exact source SHA and validates the release version;
2. builds `linux/amd64` and `linux/arm64` manifests;
3. publishes matching tags to GHCR and Docker Hub;
4. attaches OCI labels, maximum-mode provenance, and an SBOM;
5. creates a GitHub artifact attestation for the GHCR digest;
6. fails on fixed HIGH or CRITICAL vulnerabilities;
7. pulls the immutable digest and verifies non-root user and entrypoint;
8. retains digest evidence for 90 days.

The repository variable `DOCKERHUB_USERNAME` and Actions secret
`DOCKERHUB_TOKEN` are required. Never use an account password, print a token,
or commit credentials.

## Publishing and rollback

Use the workflow on `main` and supply a version without a leading `v`. Do not
reuse an existing release version for different source. If a release is bad,
consume the previous digest and publish a corrected version; do not silently
overwrite immutable evidence.

Container publication does not resolve the remaining production gaps: cloud
deployment design, backup/restore drills, SLOs, external penetration testing,
or owner-selected licensing.

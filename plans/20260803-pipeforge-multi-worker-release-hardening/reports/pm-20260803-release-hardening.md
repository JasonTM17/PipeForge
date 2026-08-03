# Release hardening status

## Delivered

- Persisted heartbeat-driven worker registry and atomic capacity-aware lease selection.
- Worker-specific job/cancellation routing with optional legacy cutover mode.
- Real single-worker and two-worker Compose proof, including cancellation.
- Hard shutdown deadline, retry boundaries, timestamp-skew protection, and adversarial re-review.
- Go 1.25 dependency security updates; 74 Python tests; unit, race, vet, integration, contract, frontend, and CodeQL gates pass.
- Strict protected `main`; force-push/deletion disabled; solo-owner bypass documented.
- Dependabot branches: green compatible updates integrated; superseded or failing major updates closed with reasons and branches deleted.

## Evidence

- CI: `30799562214`
- CodeQL: `30799562229`
- Verified source: `ddf4347`
- Required checks: 8/8 configured
- Open actionable review findings: 0

## Package

- Artifact: `release/pipeforge-0.2.0-local.zip`
- Source archive commit: `764e7b5`
- Entries: 515
- SHA-256: `53D2F3460613A7EFD3B7FB1A872C6AA50D4BB739A99C3940C5403DBF44C0E78F`
- Nested release archives: none

## Remaining

- Publish immutable `v0.2.0-local` prerelease after the package commit passes CI.

## Unresolved questions

- License choice remains an owner decision.

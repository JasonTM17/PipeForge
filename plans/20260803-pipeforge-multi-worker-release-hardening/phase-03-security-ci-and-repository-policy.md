# Phase 3: Security CI and repository policy

## Files

- Add a GitHub CodeQL workflow for Go, Python, and JavaScript/TypeScript when
  supported by the current official action.
- Add pull-request dependency review and scheduled dependency automation using
  least-privilege permissions.
- Add repository-native secret/dependency checks that fail closed without cloud
  credentials.
- Configure `main` protection only after the exact remote check names are green.

## Invariants

- Pull-request code never receives write tokens or release credentials.
- Actions use explicit minimal permissions and current official major versions.
- Required checks correspond to actual successful check-run names.
- Force-push and deletion are disabled; owner bypass remains explicit rather
  than silently assumed.

## Validation

- Workflow syntax and action configuration review.
- GitHub run evidence on the pushed commit.
- GitHub API read-back of vulnerability/security settings and branch policy.

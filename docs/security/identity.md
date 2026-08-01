# Identity and authorization controls

Phase 4 identity behavior is implemented in the Go control plane. Registration creates only `USER` principals; `ADMIN` and `SERVICE` records require an operator-controlled provisioning path until the administration phase exists.

## Credential storage

- Passwords use Argon2id with a per-password random salt. The encoded hash is stored in `users.password_hash`; plaintext passwords are never persisted.
- Access tokens are short-lived HS256 JWTs. Claims identify the user and token type; current role and scopes are loaded from PostgreSQL on each authenticated request.
- Refresh tokens are random opaque values. Only SHA-256 hashes are stored. Rotation marks the presented token used and inserts a replacement in the same transaction. Reuse revokes the entire token family.
- API keys use the `pfk_` prefix. Only the key prefix, SHA-256 hash, owner, and explicit scopes are stored. Plaintext is returned only from `POST /v1/api-keys` and is absent from list responses.

## Roles and scopes

| Role | Default behavior |
|---|---|
| `ADMIN` | Full known-scope access for bearer sessions; ownership bypass applies. An admin API key is still limited to the scopes stored on that key. |
| `USER` | Access is limited to current user scopes and owned resources. |
| `SERVICE` | Access is limited to explicitly granted scopes; no implicit ownership bypass. |

Current scopes are `datasets:read`, `datasets:write`, `jobs:read`, `jobs:write`, and `artifacts:read`. Unknown scopes and duplicate requested scopes are denied.

## HTTP behavior

- `POST /v1/auth/register` normalizes email, validates the password, creates a user, and returns a token pair.
- `POST /v1/auth/login` returns the same token shape without exposing whether a missing email or wrong password caused the failure.
- `POST /v1/auth/refresh` rotates refresh tokens. A reused token returns `401` and invalidates its family.
- `POST /v1/auth/logout` revokes the refresh-token family and writes an audit event.
- API-key management requires a valid bearer token or API key and enforces owner boundaries.
- Authentication failures use a stable `UNAUTHENTICATED` problem response with a request ID. Credential values and internal errors are not returned.

## Operational requirements

Set `JWT_SIGNING_KEY` to a random value of at least 32 bytes outside local development. Configure `PIPEFORGE_ACCESS_TOKEN_TTL` and `PIPEFORGE_REFRESH_TOKEN_TTL` through the validated environment. Apply migrations with `make migrate` before enabling the API against a fresh database.

Rate limiting and broader quota enforcement remain in the security/quotas phase; the identity handlers already bound JSON request bodies and reject unknown fields.

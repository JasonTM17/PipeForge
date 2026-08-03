# Security policy

PipeForge is an evolving portfolio project. Do not use it with real sensitive data or production credentials until the security controls and deployment model have been independently reviewed.

## Reporting

For a suspected vulnerability, do not open a public issue containing credentials, personal data, exploit payloads, or private URLs. Contact the repository owner through a private channel and include a minimal reproduction, affected component, impact, and mitigation if known.

## Development rules

- Never commit `.env`, tokens, passwords, keys, credentials, private datasets, or presigned URLs.
- Treat uploaded files, filenames, MIME types, message payloads, and operation configs as untrusted.
- Do not log passwords, API keys, object-storage secrets, complete data rows, or sensitive field values.
- Keep PostgreSQL, RabbitMQ, and MinIO credentials in environment/secret-manager injection.
- Run dependency, secret, and container scans before release.

See the implemented-control mapping in
[`docs/security/threat-model.md`](docs/security/threat-model.md) and the
deployment checklist in
[`docs/security/secure-configuration.md`](docs/security/secure-configuration.md).

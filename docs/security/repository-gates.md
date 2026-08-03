# Repository security gates

The public GitHub repository protects `main` with strict, up-to-date status
checks. Force-push and branch deletion are disabled, linear history and
conversation resolution are required, and changes use a pull request boundary.

Required checks:

- `Go checks`
- `Go integration checks`
- `Python checks`
- `Contract and repository checks`
- `Console build`
- `Analyze (go)`
- `Analyze (python)`
- `Analyze (javascript-typescript)`

CodeQL scans Go, Python, and JavaScript/TypeScript. Dependency review rejects
new moderate-or-higher vulnerable dependencies in pull requests. Dependabot
opens scheduled dependency updates. Vulnerability alerts, automated security
updates, secret scanning, and secret push protection are enabled.

This is a solo-owner repository, so zero approving reviews are required and
administrators are not subject to the protection rule. That is an explicit
emergency-maintenance bypass, not an assertion that unreviewed changes are
safe. Normal changes should still use a pull request and satisfy every required
check. Any owner bypass must be followed by green checks on the resulting
`main` commit.

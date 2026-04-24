# Project Security Policy

## Design Documentation

See [Design Documentation](../docs/DESIGN.md) for a comprehensive description
of all actors, actions, and data flows within the system.

## Confidential Disclosure

Please email [chicks.net@gmail.com](mailto:chicks.net@gmail.com) with a subject
line like "$REPONAME SECURITY issue: $SUMMARY".  Details on when you experienced
the issue, logs, and other context are appreciated to assist with effective
triaging of your issue.

## Security Updates

End users should expect new releases to include any security updates and there
should be a notification in the release notes.
We may participate in other disclosure programs as circumstances may warrant.

## Known issues

There are no known security vulnerabilities in the software at the time
this was written.

## Security Risks

We are unaware of any security risks particular to this software that you
should be aware of.  Please let us know if we missed anything or forgot to
update this section in too long.

## Dependency Management

When the project has made a release, the project documentation MUST include a
description of how the project selects, obtains, and tracks its dependencies.

### How We Select Dependencies

- Dependencies are chosen based on necessity, maturity, and maintenance activity.
- We prefer well-maintained libraries with active maintainers and transparent security practices.
- We evaluate dependencies for compatibility, license compliance, and security history before adoption.

### How We Obtain Dependencies

- All dependencies are declared in `go.mod` and fetched via `go get` / `go mod download`.
- The `go.sum` file tracks checksums to ensure integrity and reproducibility.
- `go mod tidy` is used to keep the dependency graph minimal and accurate.

### How We Track Dependencies

- `go.sum` provides cryptographic verification of all downloaded modules.
- We use `just deps-update` to regularly update dependencies and apply security patches.
- Pre-commit hooks include `gitleaks` for secret scanning and `golangci-lint` for static analysis.
- GitHub Dependabot is enabled to automatically flag and propose updates for vulnerable dependencies.

## Secrets and Credentials Management

This project defines the following policy for storing, accessing, and rotating
secrets and credentials used in development, CI/CD, and release processes.

### Secrets in Scope

| Secret | Purpose | Storage |
|--------|---------|---------|
| `GITHUB_TOKEN` | Auto-provisioned per-workflow token for GitHub Actions | GitHub Actions runtime (automatic) |
| `CLAUDE_CODE_OAUTH_TOKEN` | OAuth token for Claude Code Action | GitHub repository secret |
| `SCORECARD_TOKEN` | (Optional) PAT for OpenSSF Scorecard write access | GitHub repository secret (currently unused) |

### Storage

- **No secrets in source code**: Secrets must never be committed to the
  repository, including hardcoded values in code, configuration files, or
  documentation.
- **GitHub Secrets for CI/CD**: All secrets used in GitHub Actions workflows
  are stored as GitHub repository secrets, which are encrypted at rest and
  never exposed in logs.
- **No local secret files**: Developers must not store credentials in
  unencrypted files within the repository (e.g., `.env` files). The
  application retrieves the GitHub token at runtime via `GITHUB_TOKEN`
  environment variable or `gh auth token` — never from a file on disk.

### Access

- **Least-privilege permissions**: Every workflow declares explicit
  `permissions:` blocks scoped to the minimum required. No workflow uses
  `permissions: write-all` or broad `contents: write` unless necessary.
- **No persistent credentials in checkout**: All `actions/checkout` steps use
  `persist-credentials: false` to prevent the GITHUB_TOKEN from persisting on
  disk after the checkout step completes.
- **No secret sharing across workflows**: Each workflow references only the
  secrets it needs. Secrets are not passed between jobs or workflows unless
  required by the workflow's purpose.
- **Environment protection**: The Claude Code workflow runs in the `claude`
  GitHub environment, providing an additional access gate.

### Rotation

- **`GITHUB_TOKEN`**: Automatically rotated by GitHub Actions on every
  workflow run. No manual rotation required.
- **`CLAUDE_CODE_OAUTH_TOKEN`**: Must be rotated by a repository administrator
  when the token expires, is suspected of compromise, or when the associated
  OAuth grant is revoked. Set a calendar reminder to review quarterly.
- **Emergency rotation**: If any secret is suspected of compromise, a
  repository administrator must revoke and replace it immediately via
  GitHub repository settings, then file a security issue per the
  [Confidential Disclosure](#confidential-disclosure) process.

### Detection and Prevention

- **Pre-commit scanning**: `gitleaks` runs as a pre-commit hook to detect
  accidentally committed secrets before they reach the repository.
- **GitHub secret scanning**: GitHub's built-in secret scanning is enabled on
  the repository to detect leaked tokens in pushed commits and PRs.
- **Workflow hardening**: All workflows use pinned SHA references for third-party
  actions (not tags), `step-security/harden-runner` for egress auditing, and
  `persist-credentials: false` on checkout steps.

## Contributor Legally Authorized Assertion (DCO)

The version control system requires all code contributors to assert that they
are legally authorized to make the associated contributions on every commit.
This is enforced through the **Developer Certificate of Origin (DCO)** policy
described in [CONTRIBUTING.md](CONTRIBUTING.md).

Every commit **must** include a `Signed-off-by:` trailer (added automatically
by `git commit -s`), which affirms that the contributor agrees to the DCO
v1.1 terms.  Pull requests missing this trailer on any commit are blocked by
the [DCO GitHub App](https://github.com/apps/dco) and cannot be merged.

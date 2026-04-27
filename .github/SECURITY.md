# Project Security Policy

## Design Documentation

See [Design Documentation](../docs/DESIGN.md) for a comprehensive description
of all actors, actions, and data flows within the system.

## Threat Model and Attack Surface Analysis

See [Threat Model](../docs/THREAT_MODEL.md) for a systematic threat model
identifying trust boundaries, critical code paths, and a STRIDE analysis of
potential attacks with their mitigations.

## Confidential Disclosure

Please email [chicks.net@gmail.com](mailto:chicks.net@gmail.com) with a subject
line like "$REPONAME SECURITY issue: $SUMMARY".  Details on when you experienced
the issue, logs, and other context are appreciated to assist with effective
triaging of your issue.

## Release Support

### Scope and Duration of Support

gh-observer follows a rolling release model. Each new release includes the
latest features, bug fixes, and security updates. We support the most recent
release only — there are no long-term support (LTS) branches or parallel
maintenance streams.

The scope of support for each release includes:

- Security vulnerability fixes
- Bug fixes that affect core functionality (PR check watching, TUI rendering,
  API interaction)
- Compatibility updates for GitHub API changes and Go version requirements

We do not backport fixes to older releases. Users should upgrade to the latest
release to receive all fixes.

### End of Security Updates

A release version no longer receives security updates as soon as a newer
release is published. There is no fixed support window — the only supported
version is the latest release at any given time.

If a security vulnerability is reported, we will fix it in the next release.
We do not issue patches for older versions. Users must upgrade to the latest
release to obtain security fixes.

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

## Release Integrity and Authenticity Verification

When the project has made a release, the project documentation MUST include
instructions to verify the integrity and authenticity of the release assets.
All release binaries are signed and attested using three complementary supply
chain security mechanisms:

1. **GitHub Build Attestations** - Verifiable via `gh attestation verify`
2. **Cosign Keyless Signatures** - Verifiable via `cosign verify-blob`
3. **SLSA Provenance** - Verifiable via `slsa-verifier`

See [Verifying Release Assets](../README.md#verifying-release-assets) in the
README for complete step-by-step verification instructions.

## Verifying Release Author Identity

When the project has made a release, the project documentation MUST contain
instructions to verify the expected identity of the person or process authoring
the software release. Releases are authored through an automated process; the
following methods verify that identity.

### Verifying the Release Process Identity

Releases are created by `just release`, which runs `gh release create` using
the GitHub CLI authenticated as a repository maintainer. The release workflow
(`.github/workflows/release.yml`) then builds and signs the binaries. You can
verify that a release was produced by this authorized process:

1. **Check the release workflow run**: Every release triggers a GitHub Actions
   workflow run. Visit the release page on GitHub and follow the link to the
   workflow run that produced the binaries. Confirm the workflow file path is
   `.github/workflows/release.yml` and the triggering event is a tag push.

2. **Verify build attestations**: GitHub build attestations cryptographically
   bind each binary to the specific workflow and repository that built it:

   ```bash
   gh attestation verify darwin-arm64 --owner fini-net
   ```

   This confirms the binary was built by the `fini-net/gh-observer` release
   workflow, not by an external or unauthorized process.

3. **Verify SLSA provenance**: The `.intoto.jsonl` provenance attestation
   provides non-forgeable proof of the build origin, including the source
   repository, branch, and builder identity. See
   [Option 3: SLSA Provenance](../README.md#option-3-slsa-provenance) for
   verification commands.

### Verifying the Release Author Identity

Every commit in the repository includes a `Signed-off-by:` trailer per the
DCO policy (see [Contributor Legally Authorized Assertion](#contributor-legally-authorized-assertion-dco)
below). To verify the identity of the person who authored the release:

1. **Check the release tag commit**: View the commit the release tag points to
   and verify the author and committer identity:

   ```bash
   git log -1 --format='Author: %an <%ae>%nCommitter: %cn <%ce>%nSigned-off-by: %(trailers:key=Signed-off-by)' v1.8.0
   ```

2. **Check the GitHub release author**: On the
   [releases page](https://github.com/fini-net/gh-observer/releases), each
   release shows the GitHub username of the maintainer who created it. This
   should match an authorized repository maintainer.

3. **Cross-reference DCO signatures**: The `Signed-off-by:` trailer on each
   commit in the release range affirms the contributor's legal authorization
   under the DCO. Verify the trailer identity matches the commit author.

## Secrets and Credentials Management

This project defines the following policy for storing, accessing, and rotating
secrets and credentials used in development, CI/CD, and release processes.

### Secrets in Scope

| Secret | Purpose | Storage |
| -------- | --------- | --------- |
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

## CI/CD Input Sanitization and Validation

When the project has made a release, CI/CD pipelines which accept trusted
collaborator input MUST sanitize and validate that input prior to use in the
pipeline.

### Trusted Collaborator Inputs in CI/CD Pipelines

The following table identifies all inputs accepted by CI/CD pipelines from
trusted collaborators (repository members, PR authors, commenters) and
describes the validation applied:

| Input | Source | Workflow | Validation |
| ----- | ------ | -------- | ---------- |
| `github.repository` | GitHub (workflow context) | `claude-code-review.yml` | Regex validated against `^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+$` in explicit validation step before use |
| `github.event.pull_request.number` | GitHub (PR event) | `claude-code-review.yml` | Validated as positive integer (`^[1-9][0-9]*$`) in explicit validation step before use |
| `github.event.comment.body` | Commenters | `claude.yml` | Not interpolated into shell or action inputs; only used in `contains()` expression for trigger filtering. Author association checked to restrict to `owner`, `member`, or `collaborator` |
| `github.event.review.body` | Reviewers | `claude.yml` | Same as comment body — `contains()` filter only, author association checked |
| `github.event.issue.body`, `github.event.issue.title` | Issue authors | `claude.yml` | Same as comment body — `contains()` filter only, author association checked |
| `github.event.pull_request.number` | GitHub (PR event) | `claude.yml` | Not used in workflow (action detects PR context internally) |
| PR code changes | Fork contributors | All PR-triggered workflows | All PR workflows use `pull_request` trigger (not `pull_request_target`), running in fork context without secret access |

### Sanitization and Validation Controls

1. **No `pull_request_target` usage**: All workflows that trigger on PRs use the
   `pull_request` event, which checks out the fork's code and runs without
   access to repository secrets. This prevents untrusted PR code from
   exfiltrating secrets.

2. **Author association gating**: The `claude.yml` workflow restricts execution
   to users with `owner`, `member`, or `collaborator` author association. This
   prevents arbitrary commenters on public repositories from triggering the
   Claude Code action with secret access.

3. **Explicit validation steps**: Before event-sourced values like
   `github.repository` or PR numbers are interpolated into action inputs, they
   pass through a dedicated `Validate PR inputs` step that checks them against
   expected patterns. Only validated outputs (`steps.validate.outputs.*`) are
   used in subsequent steps.

4. **No `${{ }}` interpolation of untrusted strings in `run:` steps**: No
   workflow uses `${{ }}` expressions to interpolate untrusted event data (PR
   titles, bodies, comment text, etc.) directly into shell commands. All
   `run:` steps use static commands or trusted environment variables.

5. **Pinned action SHAs**: All third-party actions are referenced by commit SHA
   (not mutable tags), preventing tag-mutation supply chain attacks.

6. **Environment protection**: Comment-triggered workflows (`claude.yml`,
   `claude-code-review.yml`) run in the `claude` GitHub environment, which
   provides an additional approval gate before secrets are available.

7. **Harden-runner egress auditing**: Most workflows use
   `step-security/harden-runner` with `egress-policy: audit` to detect
   unexpected outbound network calls that could indicate data exfiltration.

### Summary of Protection by Threat Scenario

| Threat | Mitigation |
| ------ | ---------- |
| Script injection via PR title/body in `run:` step | No `run:` step interpolates PR-sourced data |
| Secret exfiltration via fork PR code | `pull_request` trigger runs in fork context; no `pull_request_target` |
| Arbitrary user triggering Claude Code action | `author_association` check restricts to repo collaborators |
| Malicious action version via tag mutation | All actions pinned to SHA hashes |
| Credential persistence after checkout | All `actions/checkout` steps use `persist-credentials: false` |
| Malicious input via `github.repository` or PR number | Explicit validation step with regex/integer checks |

## Permissions and Access Policy

This project requires that code collaborators be reviewed prior to
receiving escalated permissions to sensitive resources.

### Current Access Model

- **No elevated permissions for external collaborators:** Write, admin,
  and secret access are not granted to external contributors.
- **Fork-based contributions:** All external contributions are submitted
  via pull requests from personal forks. No direct push access to
  protected branches is granted to anyone outside the maintainer team.
- **Least-privilege principle:** CI workflows use scoped `permissions:`
  blocks and `persist-credentials: false` on checkout steps (see
  [Secrets and Credentials Management](#secrets-and-credentials-management)).

### Granting Elevated Permissions

If elevated permissions (write access, admin access, or access to
repository secrets) are ever considered for a collaborator, the following
policy applies:

1. The request must be reviewed by an existing repository maintainer.
2. Permissions are granted at the minimum scope necessary for the
   collaborator's role.
3. Elevated permissions are time-limited where GitHub supports it, or
   reviewed quarterly otherwise.
4. When a collaborator's role changes or they become inactive, their
   elevated permissions are revoked promptly.

## Contributor Legally Authorized Assertion (DCO)

The version control system requires all code contributors to assert that they
are legally authorized to make the associated contributions on every commit.
This is enforced through the **Developer Certificate of Origin (DCO)** policy
described in [CONTRIBUTING.md](CONTRIBUTING.md).

Every commit **must** include a `Signed-off-by:` trailer (added automatically
by `git commit -s`), which affirms that the contributor agrees to the DCO
v1.1 terms.  Pull requests missing this trailer on any commit are blocked by
the [DCO GitHub App](https://github.com/apps/dco) and cannot be merged.

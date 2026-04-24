# Project Security Policy

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

## Contributor Legally Authorized Assertion (DCO)

The version control system requires all code contributors to assert that they
are legally authorized to make the associated contributions on every commit.
This is enforced through the **Developer Certificate of Origin (DCO)** policy
described in [CONTRIBUTING.md](CONTRIBUTING.md).

Every commit **must** include a `Signed-off-by:` trailer (added automatically
by `git commit -s`), which affirms that the contributor agrees to the DCO
v1.1 terms.  Pull requests missing this trailer on any commit are blocked by
the [DCO GitHub App](https://github.com/apps/dco) and cannot be merged.

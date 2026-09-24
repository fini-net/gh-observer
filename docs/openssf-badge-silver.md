# Filling Out the OpenSSF Best Practices Silver Badge

This is the silver-tier companion to [the passing-badge guide](openssf-badge-passing.md)
— same copy-paste format, aimed at
[bestpractices.dev/projects/12633](https://www.bestpractices.dev/projects/12633).
The passing badge was achieved (2026-09-23), which satisfies the
`achieve_passing` prerequisite automatically. The silver form shows
**55 criteria, 8 already Met, 47 left** as of this writing.

The eight already-Met criteria — `achieve_passing`, `contribution_requirements`,
`report_tracker`, `tests_documented_added`, `warnings_strict`,
`crypto_weaknesses`, `static_analysis_common_vulnerabilities`,
`dynamic_analysis_unsafe` — carry over from the passing submission or were
auto-proposed from your existing justifications. **Do not re-enter those.**
The Analysis section (2/2) is complete; everything else in this doc covers the
remaining 47.

Where the passing tier was mostly "paste evidence for what the repo already
does," silver has teeth: a handful of criteria require artifacts that do not
exist yet, one is a quantitative gate the project currently fails, and one
depends on facts outside the repo (org access). Everything else — the long
tail of crypto, build, installation, and dependency criteria — is honest
Met or N/A with evidence you already have.

Work through this doc top to bottom: the artifacts section first (the form
needs URLs pointing at real files), then the per-criterion tables.

## How the silver form is organized

The form inherits your passing-tier answers, so you will see the same six
sections with higher denominators: Basics (17), Change Control (1),
Reporting (3), Quality (19), Security (13), Analysis (2). Each criterion
takes Met / Unmet / N/A (where offered) / Future, same semantics as passing:
all MUSTs Met, SHOULDs Met or Unmet-with-justification, SUGGESTEDs answered.
Save after each section — same session-timeout roulette as before.

One silver-specific note: many criteria here are **conditional MUSTs** —
"MUST, if X" — and offer N/A for when X doesn't apply. For a pure-Go CLI
with no password storage and no GUI, that N/A is the honest answer for a
surprising number of criteria. Don't be shy about it; N/A is a scored
answer, not a dodge.

## Create these artifacts first

The form requires URLs for real, public files. These four items are
prerequisites — the tables below assume they exist.

### 1. `GOVERNANCE.md` (covers `governance` + `roles_responsibilities`)

The form explicitly allows governance and roles in one document. The
criterion details say maintainer-led is fine: "In small projects, this may
be as simple as 'the project owner and lead makes all final decisions.'"
Draft — drop this in `docs/GOVERNANCE.md` and adjust the roadmap link:

```markdown
# Project Governance

gh-observer is a maintainer-led project. Final decisions on direction,
scope, and merges rest with the project owner (@chicks-net). Day-to-day,
the project runs on rough consensus: anyone may propose a change via a
pull request, and proposals are discussed openly in the PR and in issues
before the maintainer accepts or declines them.

## Roles

- **Maintainer / project owner** (@chicks-net): sets roadmap and scope,
  reviews and merges pull requests, triages issues and security reports,
  and cuts releases. Has admin on the fini-net organization and this
  repository.
- **Contributors**: anyone submitting issues, discussions, or pull
  requests. Contributions follow the requirements in
  [CONTRIBUTING.md](CONTRIBUTING.md) (DCO sign-off, tests for major
  changes, passing CI).

## Succession

The repository lives in the fini-net GitHub organization, which has
multiple organization owners. If the maintainer is incapacitated, another
organization owner can administer the repository: create and close issues,
accept proposed changes, and publish releases. The release process is
fully scripted (see the release recipe in the justfile), and signing is
keyless (Sigstore), so no private signing keys need to be handed over.

## Decision disputes

Disagreements are resolved by discussion in the relevant issue or PR; if
discussion does not produce agreement, the maintainer decides.
```

### 2. `docs/ROADMAP.md` (covers `documentation_roadmap`)

"Need not be detailed" per the criterion details — one page of intents.
Draft:

```markdown
# Roadmap

This is a maintained best-effort statement of direction for the next year.
It is not a commitment; items may be delivered out of order or dropped.

## In scope

- Continue refining the three watch modes (PR, run, repo) — output
  clarity, startup-phase handling, and queue-latency accuracy
- Keep pace with GitHub API changes (new check types, new Actions
  features) so the display stays trustworthy
- TUI quality-of-life: filtering, sorting, and keyboard navigation
- Increase automated test coverage, particularly in the TUI layer
- Performance of repo mode for repositories with many active PRs

## Out of scope (for now)

- Live-streaming job logs — blocked on a GitHub API gap; tracked in
  issue #127
- A GUI or web frontend; gh-observer is a terminal tool
- Watching providers other than GitHub

## Maintenance

Security updates and dependency refreshes continue regardless of the
feature roadmap.
```

### 3. Coding standards section in CONTRIBUTING.md (covers `coding_standards` + `coding_standards_enforced`)

Neither CONTRIBUTING.md nor the README names a style guide today. Add a
short section to CONTRIBUTING.md, e.g. after "Development requirements":

```markdown
## Coding standards

Go code follows the standard Go conventions: gofmt formatting and the
style guidance in Effective Go
(<https://go.dev/doc/effective_go>). Contributions are expected to be
gofmt-clean; this is enforced automatically by golangci-lint in
pre-commit and CI, along with a broader linter set. Shell scripts must
pass shellcheck.
```

### 4. Secure-design-principles section in THREAT_MODEL.md (covers `implement_secure_design` + `assurance_case`)

The threat model already nails three of the four assurance-case elements
(threat model, trust boundaries, implementation-weakness countermeasures).
Add a short "Secure Design Principles" section arguing each principle. A
paste-ready mapping (verify each claim against the current code as you
edit):

```markdown
## Secure Design Principles

- **Least privilege**: the tool requests no OAuth scopes and reads only
  the ambient `GITHUB_TOKEN` / `gh` CLI token; it runs with the user's
  normal privileges and installs nothing privileged.
- **Fail-safe defaults**: unknown check conclusions render as "unknown"
  rather than being treated as success; on transient API errors the TUI
  retains the last good state instead of guessing; snapshot mode exits
  non-zero when in doubt.
- **Complete mediation**: every byte from the GitHub API and every CLI
  argument passes through validated parsing (anchored regexes for URLs,
  typed GraphQL/REST decoding) before it reaches display logic.
- **Economy of mechanism**: single binary, no plugins, no network
  listeners; the only subprocess invoked is `gh` for token/auth
  delegation.
```

## The coverage gate (read before answering Quality)

`test_statement_coverage80` is a MUST with no dodge available — Go has
FLOSS coverage tooling, and a pure-Go project cannot claim N/A. Current
numbers from `go test -cover ./...`:

| Package | Coverage |
| --- | --- |
| `internal/config` | 97.4% |
| `internal/debug` | 93.6% |
| `internal/timing` | 97.1% |
| `internal/github` | 65.9% |
| `internal/tui` | 54.0% |
| `main` (root) | 0.0% |

Statements are not weighted equally across packages, but the TUI layer is
the bulk of the code, so the honest whole-project number is roughly
**68%** — well short of 80%. Treat this as **the** blocker for silver:

until coverage genuinely clears 80%, this criterion is Unmet, and a MUST
Unmet means no silver badge.

The gap is concentrated and very testable — these are deterministic
pure-ish functions, not live API calls:

- `internal/tui/repoview.go` — nearly the entire repo-mode render path is
  at 0%: `renderPRGroup`, `renderRepoCheckRun`, `renderStandaloneRunsSection`,
  `renderBranchGroup`, `renderBranchRunHeader`, `renderBranchRunJob`,
  `styleForCheck`, `formatBranchRunDuration`, `formatBranchJobName(Truncate)`,
  `formatBranchJobDuration`, `calculateBranchRunColumnWidths`,
  `groupBranchRunsByBranch`, `sortedBranchNames`. All take model state and
  return strings — table-driven tests, no mocks needed.
- `internal/tui/runupdate.go` — `handleRunJobsUpdate`,
  `handleRunWorkflowsDiscovered`, `handleRunJobAveragesPartial` at 0%;
  construct messages, feed them through `Update`, assert on model state.
- `internal/tui/model.go:NewModel/ExitCode`, `repomodel.go:ExitCode`,
  `runmodel.go:NewRunModel/ExitCode` — constructors and exit-code
  calculation, trivial wins.
- `internal/github` live-network functions at 0% (`FetchPRInfo`,
  `FetchRunJobs`, `FetchCheckRunsGraphQL`, etc.) — these hit real
  endpoints; cover via the existing parse/decode test pattern (feed
  recorded JSON) rather than live calls. `WorkflowJobInfoToCheckRuns`
  (`runs.go:276`) is pure translation logic — the cheapest big win in
  the package.
- `main` at 0% — thin Cobra wiring; low statement count, but if the
  numbers are tight after the TUI work, `parseArgs` table tests close it.

The other two test criteria in Quality (`regression_tests_added50`,
`test_policy_mandated`) do not depend on the coverage number and can be
answered now — see the table below.

## Per-criterion answers

Same conventions as the passing guide: level, answer, paste-ready
justification, evidence URL. Skip anything listed as already Met at the
top of this doc.

### Basics (15 remaining)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 1 | `dco` (SHOULD) (URL) | Met | Every commit must carry a `Signed-off-by:` DCO trailer; the DCO GitHub App blocks PRs missing it. CONTRIBUTING.md explains the requirement and links the DCO website. | `blob/main/.github/CONTRIBUTING.md` |
| 2 | `governance` (MUST) (URL) | Met | Governance is documented in docs/GOVERNANCE.md: maintainer-led model, open discussion in PRs/issues, maintainer as final decider. | `blob/main/docs/GOVERNANCE.md` |
| 3 | `code_of_conduct` (MUST) (URL) | Met | The project adopts the Contributor Covenant code of conduct, posted in the standard `.github/` location. | `blob/main/.github/CODE_OF_CONDUCT.md` |
| 4 | `roles_responsibilities` (MUST) (URL) | Met | Roles and responsibilities are documented in docs/GOVERNANCE.md (maintainer and contributor roles, their tasks, and who holds them); governance and roles share one document, which the criterion explicitly permits. | `blob/main/docs/GOVERNANCE.md` |
| 5 | `access_continuity` (MUST) (URL) | **TODO** | See the dedicated section below — depends on fini-net org admin roles, not on the repo. | (see below) |
| 6 | `bus_factor` (SHOULD) (URL) | **Unmet, with justification** | The bus factor is 1: a single maintainer authors essentially all non-automated commits. This is acknowledged rather than papered over — the succession section of docs/GOVERNANCE.md documents the path for continuity, and the intent is to grow co-maintainers as the contributor base grows. | `blob/main/docs/GOVERNANCE.md` |
| 7 | `documentation_roadmap` (MUST) (URL) | Met | docs/ROADMAP.md describes intended and out-of-scope work for the next year. | `blob/main/docs/ROADMAP.md` |
| 8 | `documentation_architecture` (MUST) (URL) | Met | docs/DESIGN.md documents the architecture: major components (CLI, GitHub API layer, TUI, config, timing), their relationships, and data flows; it was written to satisfy this badge's design-documentation requirement. | `blob/main/docs/DESIGN.md` |
| 9 | `documentation_security` (MUST) (URL) | Met | The security policy documents what users can and cannot expect: the threat model defines the guarantees (validating all API input, TLS-only transport, no credential persistence), and SECURITY.md defines support scope, disclosure, and the VEX process. | `blob/main/docs/THREAT_MODEL.md` |
| 10 | `documentation_quick_start` (MUST) (URL) | Met | The README opens with a one-command quick start: install as a gh extension (`gh extension install fini-net/gh-observer`), run `gh observer` to auto-detect the current PR — working output in under a minute. | `blob/main/README.md` |
| 11 | `documentation_current` (MUST) | Met | Documentation is updated in the same PRs as the code changes it describes; known defects are fixed when found (the previously-broken support link was fixed). | `blob/main/README.md` |
| 12 | `documentation_achievements` (MUST) (URL) | Met | The README front page displays the OpenSSF Scorecard badge and the Best Practices badge, hyperlinked to their project pages, added when the passing badge was achieved. | `blob/main/README.md` |
| 13 | `accessibility_best_practices` (SHOULD) | **Unmet, with justification** | A TUI with checkmark/cross icons and color-coded statuses cannot fully meet WCAG-style guidance: color is used alongside symbols, but terminal constraints prevent full text-alternative and contrast guarantees. Screen-reader users can still use snapshot mode (plain text, pipe-friendly). Progress here is incremental and tracked in issues. | (none) |
| 14 | `internationalization` (SHOULD) | **Unmet, with justification** | The TUI generates English-only output; no i18n framework is wired in. The user-facing string surface is small and fixed, and localization is not on the roadmap for the next year. Acceptable as an honest SHOULD miss. | (none) |
| 15 | `sites_password_security` (MUST) | N/A | All project sites are GitHub properties; GitHub handles authentication and password storage for the project. The project itself stores no passwords. | (none) |

### Change Control (1 remaining)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 16 | `maintenance_or_update` (MUST) | Met | Users can always install the latest release (`gh extension upgrade`, `go install @latest`); older releases remain available on the releases page, and upgrade is a single command — no migration steps, config format is backward compatible. SECURITY.md documents the release support scope. | `blob/main/.github/SECURITY.md` |

### Reporting (2 remaining)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 17 | `vulnerability_report_credit` (MUST) (URL) | N/A | No vulnerability reports have been resolved in the last 12 months. | (none) |
| 18 | `vulnerability_response_process` (MUST) (URL) | Met | SECURITY.md documents the response process: confidential disclosure channel, acknowledgment goal, SCA/SAST remediation thresholds with SLAs (14-day goal for critical), and the VEX workflow for tracking non-exploitable findings. | `blob/main/.github/SECURITY.md` |

### Quality (17 remaining)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 19 | `coding_standards` (MUST) (URL) | Met | CONTRIBUTING.md identifies the coding standards: standard Go conventions (gofmt, Effective Go) for Go code, shellcheck compliance for shell scripts. | `blob/main/.github/CONTRIBUTING.md` |
| 20 | `coding_standards_enforced` (MUST) | Met | Enforcement is automatic: golangci-lint runs in pre-commit and CI (reviewdog comments on PRs); shellcheck runs via pre-commit and CI; CI fails on findings. | `blob/main/.pre-commit-config.yaml` |
| 21 | `build_standard_variables` (MUST) | N/A | The standard Go toolchain (`go build`) handles compiler and linker environment variables (CGO_ENABLED, GOFLAGS, GOOS/GOARCH, and via GOFLAGS, CC/CFLAGS/LDFLAGS passing) natively; the project adds no build system layer that could drop or override them. | (none) |
| 22 | `build_preserve_debug` (SHOULD) | N/A | `go build` embeds DWARF debug info in the binary by default; the project never strips it (`no -ldflags "-s -w"` in build or release recipes). | `blob/main/justfile` |
| 23 | `build_non_recursive` (MUST NOT) | Met | The Go build system is package-based with accurate, compiler-verified dependency information; there is no recursive make-style directory walking. | `blob/main/go.mod` |
| 24 | `build_repeatable` (MUST) | **Unmet, with justification** | Releases are not bit-for-bit reproducible: the release workflow builds with default settings and Go embeds build metadata (module version, VCS info) in binaries, and different build platforms produce different binaries. Achieving verified reproducible builds (e.g. `-trimpath`, hermetic toolchains) is a known gap; SLSA provenance provides an audit trail for the official builds in the meantime. | (none) |
| 24b | `test_statement_coverage80` (MUST) | **Unmet — see the coverage gate** | The honest current statement coverage is roughly 68% (`go test -cover ./...`), below the required 80%. This is the active blocker; the work plan above targets the repo-mode render path and run-mode update handlers first. | (none) |
| 25 | `installation_common` (MUST) | Met | Installation and uninstallation use common conventions: `gh extension install/uninstall` (the gh CLI's package mechanism), `go install` for Go users, and precompiled release binaries for manual installation. | `blob/main/README.md` |
| 26 | `installation_standard_variables` (MUST) | N/A | Installation is via the gh extension mechanism or `go install`, both of which honor GOBIN/GOPATH conventions for install location; the project performs no custom file installation that would need DESTDIR. | (none) |
| 27 | `installation_development_quick` (MUST) | Met | A developer gets the full environment with standard commands: `git clone`, `go mod download` (or just `go build` — modules fetch automatically), `just build`, `go test ./...` — all documented in CONTRIBUTING.md's development process and the README. | `blob/main/.github/CONTRIBUTING.md` |
| 28 | `external_dependencies` (MUST) (URL) | Met | External dependencies are listed in go.mod/go.sum, the computer-processable standard for Go. | `blob/main/go.mod` |
| 29 | `dependency_monitoring` (MUST) | Met | Dependencies are monitored continuously: govulncheck runs daily (and on every PR) for known Go vulnerabilities; dependency-review scans changed manifests on every PR; Dependabot and Renovate both propose updates on a schedule; findings are triaged per SECURITY.md thresholds, and OpenVEX documents non-exploitable ones. | `blob/main/.github/workflows/govulncheck.yaml` |
| 30 | `updateable_reused_components` (MUST) | Met | All external components are declared in go.mod and updated with `go get -u` / `just deps-update`; there are no vendored or forked convenience copies (no `vendor/` directory), so security updates flow in via a single standard mechanism. | `blob/main/go.mod` |
| 31 | `interfaces_current` (SHOULD) | Met | Renovate and Dependabot keep dependencies current, and the codebase is regularly modernized; deprecations surface via golangci-lint and CodeQL findings and are addressed in normal maintenance. | `blob/main/.github/renovate.json` |
| 32 | `automated_integration_testing` (MUST) | Met | The CI workflow applies the full automated test suite (`go test ./...`) on every push to main (the shared trunk) and every pull request, and reports pass/fail on each run — visible on every commit. | `blob/main/.github/workflows/ci.yml` |
| 33 | `regression_tests_added50` (MUST) | **Verify, then Met** | Recent bug-fix PRs routinely pair the fix with tests: e.g. the fade-out window edge case fix added repoupdate tests; URL parsing fixes extend the fuzz corpora. Audit the last six months of fix PRs before submitting — see "Pre-checks" below. | `blob/main/README.md` |
| 34 | `test_policy_mandated` (MUST) | Met | The policy is formal and written: CONTRIBUTING.md's Testing section mandates tests for major changes, states CI must pass, and PRs without test updates "may be asked to add tests before merging"; the README echoes it. | `blob/main/.github/CONTRIBUTING.md` |
| 35 | `warnings_strict` | — | Already Met (carried over from passing). | — |

### Security (12 remaining)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 36 | `implement_secure_design` (MUST) | Met | The threat model's Secure Design Principles section maps each principle to the implementation: least privilege (ambient token only, no OAuth), fail-safe defaults (unknown states render as unknown; last-good display retained), complete mediation (all API input through validated parsing), economy of mechanism (single binary, no listeners). | `blob/main/docs/THREAT_MODEL.md` |
| 37 | `crypto_algorithm_agility` (SHOULD) | **Unmet, with justification** | The project uses the Go standard library's TLS and SHA-256 via GitHub API transport and release checksums; it performs no user-configurable cryptography of its own, so there is no algorithm agility to expose. Switching algorithms is a toolchain/stdlib matter that would follow a Go release — acceptable for a consumer of transport crypto rather than a provider. | (none) |
| 38 | `crypto_credential_agility` (MUST) | Met | Credentials are never embedded in code, config, or logs: the token comes from the `GITHUB_TOKEN` environment variable or is delegated to the gh CLI's own secure storage at runtime; users can rotate either without any recompilation. | `blob/main/README.md` |
| 39 | `crypto_used_network` (SHOULD) | Met | All network communication is HTTPS (TLS 1.2+, preferring 1.3) to api.github.com and github.com; no plaintext protocols are used or supported anywhere. | `blob/main/docs/THREAT_MODEL.md` |
| 40 | `crypto_tls12` (SHOULD) | Met | Go's crypto/tls defaults to TLS 1.3 minimum on modern Go (1.26); connections to GitHub use at least TLS 1.2 in all configurations. | `blob/main/docs/THREAT_MODEL.md` |
| 41 | `crypto_certificate_verification` (MUST) | Met | Go's HTTP client performs standard TLS certificate verification on every request by default; the project never disables it (no `InsecureSkipVerify` anywhere in the codebase). | `blob/main/docs/THREAT_MODEL.md` |
| 42 | `crypto_verification_private` (MUST) | Met | Certificate verification happens before any HTTP headers are sent, including the Authorization header with the private token — this is Go's standard TLS handshake-then-request ordering, with verification never disabled. | `blob/main/docs/THREAT_MODEL.md` |
| 43 | `signed_releases` (MUST) | Met | Every release binary is cryptographically signed three independent ways: Sigstore keyless cosign signatures, GitHub build attestations, and SLSA L3 provenance; the README documents how to obtain the public verification material and verify each (cosign verify-blob, gh attestation verify, slsa-verifier). No private signing key is stored on distribution sites — signing is keyless via the release workflow's OIDC identity. | `blob/main/README.md` |
| 44 | `version_tags_signed` (SUGGESTED) | **Future** | Release tags are not GPG-signed; release binaries are signed (cosign/attestations/SLSA, see signed_releases), and the release tag commit carries the DCO sign-off. Signing the tags themselves would add a fourth mechanism and is a reasonable future hardening step. | (none) |
| 45 | `input_validation` (MUST) | Met | All potentially untrusted input is allowlist-validated: PR/run/repo arguments must match fully-anchored URL or slug regexes; API responses pass through typed decoding with explicit error handling; config values are validated on load. The fuzz targets exercise the parsers with arbitrary input. | `blob/main/docs/THREAT_MODEL.md` |
| 46 | `hardening` (SHOULD) | Met | Hardening measures: Go's memory-safe runtime and bounds checking; input allowlists on all untrusted data; no network listeners, no plugin loading, no dynamic eval; minimal CI/CD permissions and hardened workflow runners (harden-runner) limit the build path itself. | `blob/main/docs/THREAT_MODEL.md` |
| 47 | `assurance_case` (MUST) (URL) | Met | The assurance case is docs/THREAT_MODEL.md: it contains the threat model (STRIDE per trust boundary), clearly identified trust boundaries (six crossings, each with mitigations), an argument that secure design principles are applied (Secure Design Principles section), and an argument that common implementation weaknesses are countered (critical code paths CP-1..CP-6 map each weakness to its countermeasure). | `blob/main/docs/THREAT_MODEL.md` |

### Analysis (0 remaining)

Complete — both criteria carried over as Met.

## TODO: `access_continuity`

This MUST has no N/A, and its truth lives in the fini-net GitHub
organization, not in this repository. The claim you need to be able to
make: if the maintainer is hit by a bus, someone else can administer the
repo — create/close issues, accept changes, publish releases — **within a
week**, with minimal interruption.

Pre-drafted Met justification, ready for when the facts are confirmed:

> The repository lives in the fini-net GitHub organization with multiple
> organization owners. If the maintainer is unable to continue, another
> organization owner can administer the repository within a week: issue
> administration and merges require only GitHub UI access, and releases
> are fully scripted (the release recipe wraps `gh release create`), so
> no private keys or special local environments are needed — release
> signing is keyless (Sigstore OIDC). The governance document's
> succession section documents this.

To confirm the facts, check who else holds org-level admin. For each
candidate (e.g. the other org members you know of):

```bash
gh api orgs/fini-net/memberships/<username> --jq '.role'
```

If a second `admin` exists, answer Met with the justification above and
use `docs/GOVERNANCE.md` as the URL. If it genuinely is a one-admin org,
this criterion is honestly Unmet — and as a MUST, that blocks silver
until an org role is added. Adding a second org owner is cheap and good
practice anyway; this doc will be ready either way. Until then this
stays a **TODO**.

## Pre-checks only you can do

- **33 `regression_tests_added50`** — the criterion: at least 50% of bugs
  fixed in the last six months got regression tests. There are ~49
  fix-labeled commits in that window. Spot-check a sample of merged fix
  PRs: did the PR add/extend a test or fuzz seed? If yes for half or
  more, answer Met; if a quick audit says otherwise, answer honestly and
  cite the audit.
- **11 `documentation_current`** — do one full README read-through
  for drift (flags, output examples, config keys). Fix anything stale
  before answering.
- **5 `access_continuity`** — see the TODO section above.
- **Coverage number freshness** — re-run `go test -cover ./...` before
  answering `test_statement_coverage80`; if you've been landing tests,
  the number may have moved. The criterion will be honest whenever you
  submit it.

## After you submit

- Section counters should read full, and the project page flips from
  "passing" to "silver" once every MUST is Met (including coverage at
  80% and access continuity confirmed).
- Embed with the same badge snippet as before — the badge image itself
  updates to reflect the level:
  `[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12633/badge)](https://www.bestpractices.dev/projects/12633)`
- Revisit triggers specific to this tier's answers: if a vulnerability is
  ever resolved, `vulnerability_report_credit` flips from N/A and needs
  reporter credit; if coverage slips below 80 after a feature push, the
  badge claim is stale; and if the second org admin ever leaves,
  `access_continuity` needs re-confirmation.
- Gold tier exists, but one thing at a time — gold adds multi-person
  review requirements this project doesn't meet yet.

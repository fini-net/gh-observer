# Filling Out the OpenSSF Best Practices Badge

This is a copy-paste companion for working through the badge form at
[bestpractices.dev/projects/12633](https://www.bestpractices.dev/projects/12633).
The passing badge has since been achieved (2026-09-23) — see
[the silver-tier companion](openssf-badge-silver.md) for the next level.
This guide remains as the reference for how the passing answers were
filled in.

The goal here is the classic **passing** badge. The project page also shows
"Baseline Level 1/2/3" meters; that's a separate, newer OpenSSF Baseline
initiative and out of scope for this document.

With this doc open in one window and the form in another, the whole thing
should take under an hour.

## How the form works

- Log in at <https://www.bestpractices.dev/> using the GitHub account that
  owns the project, open the project page, and click **Edit**.
- The passing tier has 67 criteria in six sections: Basics (13), Change
  Control (9), Reporting (8), Quality (13), Security (16), Analysis (8).
  The section counters (`0/13`, etc.) tell you what's left.
- Each criterion takes one of four answers: **Met**, **Unmet**, **N/A**
  (where offered), or **Future**. To earn the passing badge: all MUST and
  MUST NOT criteria Met, all SHOULD criteria Met *or* Unmet with a
  justification, and all SUGGESTED criteria at least answered (Met, Unmet,
  or Future).
- Many criteria have a URL field — paste the evidence link exactly where
  this doc says `(URL)`. Use `blob/main/...` links so they stay stable.
- Save after each section. The form is long and you do not want to find out
  what its session timeout feels like the hard way. (Ask me how I know.)
- If you enter justification text that is a generic comment rather than a
  rationale, start it with `//` and a space.

## Quick facts for the General section

These are the short-answer fields at the top of the Basics section, before
the scored criteria.

| Field | Answer |
| --- | --- |
| Human-readable name | `gh-observer` |
| Brief description | Keep the existing one (terminal UI for watching GitHub Actions workflows with runtime metrics, queue times, and check status — alternative to `gh pr checks --watch`) |
| Project URL | `https://github.com/fini-net/gh-observer` |
| VCS repository URL | `https://github.com/fini-net/gh-observer` (same thing, and that's fine) |
| License | `GPL-2.0-only` (pick it from the SPDX dropdown) |
| Implementation languages | `Go` |
| CPE name | Leave blank — this project has no CPE entry |

## Basics (13 criteria)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 1 | `description_good` (MUST) | Met | The README succinctly explains what the tool does and what problem it solves: a better `gh pr checks --watch` with runtime metrics, queue latency, and startup-delay handling, written for potential users. | `blob/main/README.md` |
| 2 | `interact` (MUST) | Met | The README documents how to obtain the software (three install methods), how to provide feedback (GitHub issues, including security issues via SECURITY.md), and how to contribute (link to CONTRIBUTING.md). | `blob/main/README.md` |
| 3 | `contribution` (MUST) (URL) | Met | The project uses GitHub pull requests and an issue tracker; CONTRIBUTING.md documents the step-by-step contribution process. | `blob/main/.github/CONTRIBUTING.md` |
| 4 | `contribution_requirements` (SHOULD) (URL) | Met | CONTRIBUTING.md states the requirements for acceptable contributions: DCO `Signed-off-by:` trailer on every commit, tests required for major changes, and code style enforced via pre-commit hooks and CI linting. | `blob/main/.github/CONTRIBUTING.md` |
| 5 | `floss_license` (MUST) | Met | Released under GPL-2.0-only, an OSI-approved and FSF-free license. Full text is in the top-level LICENSE file. | `blob/main/LICENSE` |
| 6 | `floss_license_osi` (SUGGESTED) | Met | GPL-2.0 is approved by the Open Source Initiative. | `blob/main/LICENSE` |
| 7 | `license_location` (MUST) (URL) | Met | The license is posted as a top-level file named LICENSE, the standard location. | `blob/main/LICENSE` |
| 8 | `documentation_basics` (MUST) | Met | The README covers installation (precompiled binary, `go install`, build from source), how to start and use every mode with examples, configuration, and authentication (token handling); SECURITY.md covers secure use. | `blob/main/README.md` |
| 9 | `documentation_interface` (MUST) | Met | The external interface is a CLI. The README documents every mode (PR number, PR URL, Actions run URL, `--repo`, `--quick`) with flags, example output, and config keys; `--help` output is generated from the same definitions; `.config.example.yaml` documents all settings. | `blob/main/README.md` |
| 10 | `sites_https` (MUST) | Met | The project website, repository, and download URLs are all GitHub HTTPS URLs. | `https://github.com/fini-net/gh-observer` |
| 11 | `discussion` (MUST) | Met | GitHub issue and pull request discussions: searchable, URL-addressable, open to new participants, no proprietary client required. | `https://github.com/fini-net/gh-observer/issues` |
| 12 | `english` (SHOULD) | Met | All documentation, code comments, and issue discussions are in English. | `blob/main/README.md` |
| 13 | `maintained` (MUST) | Met | The project is actively maintained: 42 tagged releases through v4.0, frequent commits, and issues are triaged. | `https://github.com/fini-net/gh-observer/releases` |

## Change Control (9 criteria)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 14 | `repo_public` (MUST) | Met | The source repository is publicly readable at the project URL. | `https://github.com/fini-net/gh-observer` |
| 15 | `repo_track` (MUST) | Met | Git tracks what changed, who changed it, and when for every commit. | `https://github.com/fini-net/gh-observer/commits/main/` |
| 16 | `repo_interim` (MUST) | Met | Development happens on `main` with per-PR branches; all interim versions between releases are public in the repository. | `https://github.com/fini-net/gh-observer/commits/main/` |
| 17 | `repo_distributed` (SUGGESTED) | Met | The project uses git. | `https://github.com/fini-net/gh-observer` |
| 18 | `version_unique` (MUST) | Met | Every release has a unique version identifier: a git tag (v0.1 through v4.0). | `https://github.com/fini-net/gh-observer/tags` |
| 19 | `version_semver` (SUGGESTED) | Met | Tags use `vMAJOR.MINOR(.PATCH)` semantic-versioning style. | `https://github.com/fini-net/gh-observer/tags` |
| 20 | `version_tags` (SUGGESTED) | Met | Each release is identified in the version control system with a git tag — 42 tags to date. | `https://github.com/fini-net/gh-observer/tags` |
| 21 | `release_notes` (MUST) (URL) | Met | Every release on the GitHub releases page has human-readable release notes summarizing merged pull requests (generated from labeled PRs via `gh release create --generate-notes`). | `https://github.com/fini-net/gh-observer/releases` |
| 22 | `release_notes_vulns` (MUST, N/A) | N/A | No publicly known runtime vulnerabilities with CVE assignments have been fixed in any release — no CVEs exist for this project. Revisit if one is ever assigned. | `https://github.com/fini-net/gh-observer/releases` |

## Reporting (8 criteria)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 23 | `report_process` (MUST) (URL) | Met | Users submit bug reports via the GitHub issue tracker, which provides templates for bug reports and feature requests; SECURITY.md covers security-sensitive reports. | `https://github.com/fini-net/gh-observer/issues` |
| 24 | `report_tracker` (SHOULD) | Met | GitHub's issue tracker is used for individual issues. | `https://github.com/fini-net/gh-observer/issues` |
| 25 | `report_responses` (MUST) | Met | The maintainer acknowledges and triages the majority of bug reports; responses need not include fixes. | `https://github.com/fini-net/gh-observer/issues?q=is%3Aissue` |
| 26 | `enhancement_responses` (SHOULD) | Met | The maintainer responds to enhancement requests, including requests that get declined with rationale. | `https://github.com/fini-net/gh-observer/issues?q=is%3Aissue+label%3Aenhancement` |
| 27 | `report_archive` (MUST) (URL) | Met | Issues and pull requests are publicly archived and searchable. | `https://github.com/fini-net/gh-observer/issues?q=` |
| 28 | `vulnerability_report_process` (MUST) (URL) | Met | SECURITY.md publishes the process for reporting vulnerabilities, including confidential disclosure. | `blob/main/.github/SECURITY.md` |
| 29 | `vulnerability_report_private` (MUST) (URL) | Met | SECURITY.md's Confidential Disclosure section provides a private email channel for vulnerability reports. | `blob/main/.github/SECURITY.md` |
| 30 | `vulnerability_report_response` (MUST, N/A) | N/A | No vulnerability reports have been received in the last 6 months. | (none) |

## Quality (13 criteria)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 31 | `build` (MUST) | Met | `just build` (wrapping `go build`) rebuilds the binary from modified source; documented in the README. | `blob/main/README.md` |
| 32 | `build_common_tools` (SUGGESTED) | Met | Built with the standard Go toolchain; task automation via `just`, a widely used FLOSS command runner. | `blob/main/justfile` |
| 33 | `build_floss_tools` (SHOULD) | Met | The entire build toolchain — Go compiler, just, pre-commit — is FLOSS. | `blob/main/go.mod` |
| 34 | `test` (MUST) | Met | 143 test functions across all five internal packages, including 7 fuzz targets, run with standard `go test ./...` and documented in the README and CONTRIBUTING.md; CI runs the suite on every push and pull request. | `blob/main/.github/workflows/ci.yml` |
| 35 | `test_invocation` (SHOULD) | Met | Standard Go invocation: `go test ./...` (wrapped as `just test`). | `blob/main/README.md` |
| 36 | `test_most` (SUGGESTED) | Met | Unit tests cover every internal package: timing calculations, GitHub API parsing (GraphQL and REST), TUI state and display logic for all three modes, config, and debug logging, plus fuzz coverage of the URL/timestamp parsers. | `blob/main/README.md` |
| 37 | `test_continuous_integration` (SUGGESTED) | Met | The CI workflow runs the full test suite on every push to main and every pull request. | `blob/main/.github/workflows/ci.yml` |
| 38 | `test_policy` (MUST) | Met | CONTRIBUTING.md states that all major changes must add or update tests that verify the changed functionality. | `blob/main/.github/CONTRIBUTING.md` |
| 39 | `tests_are_added` (MUST) | Met | Recent major features each shipped with new test files, e.g. repo mode brought `internal/tui/repoview_test.go` and `internal/tui/repoupdate_test.go`; run mode brought `internal/tui/runview_test.go`. | `blob/main/internal/tui/` |
| 40 | `tests_documented_added` (SUGGESTED) | Met | The test-adding policy is written down in CONTRIBUTING.md and echoed in the README's Testing section. | `blob/main/.github/CONTRIBUTING.md` |
| 41 | `warnings` (MUST) | Met | golangci-lint runs via pre-commit hooks and CI (reviewdog comments on PRs); `go vet` runs as part of `go test`; the Go compiler fails on a broad class of common mistakes by default. | `blob/main/.pre-commit-config.yaml` |
| 42 | `warnings_fixed` (MUST) | Met | Lint findings block local commits via pre-commit and are surfaced on PRs via reviewdog; the codebase is kept warning-clean. | `blob/main/.github/workflows/reviewdog.yml` |
| 43 | `warnings_strict` (SUGGESTED) | Met | Go's compiler is strict by default (unused variables/imports and type errors are fatal), supplemented by golangci-lint's linter set and `go vet` on every test run. | `blob/main/.pre-commit-config.yaml` |

## Security (16 criteria)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 44 | `know_secure_design` (MUST) | Met | The maintainer authored a full STRIDE threat model (docs/THREAT_MODEL.md), design documentation covering all actors and data flows (docs/DESIGN.md), and a security policy (SECURITY.md) covering CI/CD hardening, secrets management, and dependency management. | `blob/main/docs/THREAT_MODEL.md` |
| 45 | `know_common_errors` (MUST) | Met | THREAT_MODEL.md enumerates the common error classes for this domain — token leakage, injection via untrusted CI input, TLS misuse, rate-limit side channels — mapped to STRIDE categories. | `blob/main/docs/THREAT_MODEL.md` |
| 46 | `crypto_published` (MUST) | Met | All cryptographic functionality uses publicly documented, standard primitives from the Go standard library (TLS, SHA-256) and Sigstore (keyless signing); no homegrown crypto. | `blob/main/.github/SECURITY.md` |
| 47 | `crypto_call` (MUST) | Met | Crypto is called from well-maintained FLOSS implementations: the Go standard library for TLS/hashing and cosign/Sigstore for release signing. | `blob/main/go.mod` |
| 48 | `crypto_floss` (MUST) | Met | All cryptographic implementations used (Go stdlib, Sigstore/cosign) are FLOSS. | `blob/main/go.mod` |
| 49 | `crypto_keylength` (MUST, N/A) | N/A | gh-observer does not generate or manage cryptographic keys; TLS uses GitHub's certificates and release signing uses Sigstore keyless ECDSA P-256. | (none) |
| 50 | `crypto_working` (MUST, N/A) | Met | The application depends on TLS to api.github.com for every poll — it would be non-functional if the crypto did not work, and its release signatures verify per the README instructions. | `blob/main/README.md` |
| 51 | `crypto_weaknesses` (MUST, N/A) | Met | govulncheck runs daily in CI and flags known cryptographic weaknesses in the standard library and dependencies; connections use modern TLS (1.2+ with ECDHE, 1.3 preferred) only. | `blob/main/.github/workflows/govulncheck.yaml` |
| 52 | `crypto_pfs` (MUST, N/A) | Met | All TLS sessions use Go's crypto/tls with ephemeral (ECDHE) key exchange, providing perfect forward secrecy. | `blob/main/docs/THREAT_MODEL.md` |
| 53 | `crypto_password_storage` (MUST, N/A) | N/A | gh-observer stores no passwords or credentials; it reads `GITHUB_TOKEN` from the environment or delegates to the gh CLI's authentication at runtime. | `blob/main/README.md` |
| 54 | `crypto_random` (MUST, N/A) | N/A | No randomness is used; the application is a deterministic poller. | (none) |
| 55 | `delivery_mitm` (MUST) | Met | The software is delivered via GitHub over HTTPS, which counters MITM attacks; binaries are additionally cosign-signed and carry GitHub build attestations. | `https://github.com/fini-net/gh-observer/releases` |
| 56 | `delivery_unsigned` (MUST NOT) | Met | Hashes and signatures are only distributed over HTTPS on the GitHub releases page; binaries carry cosign keyless signatures, build attestations, and SLSA provenance (verification steps documented in the README). | `blob/main/README.md` |
| 57 | `vulnerabilities_fixed_60_days` (MUST) | Met | There are no publicly known unpatched vulnerabilities of medium or higher severity; SECURITY.md commits to remediation SLAs (14-day goal for critical findings) and OpenVEX statements track non-exploitability. | `blob/main/.github/SECURITY.md` |
| 58 | `vulnerabilities_critical_fixed` (SHOULD) | Met | SECURITY.md commits to fixing critical vulnerabilities rapidly (14-day remediation goal); none are known to exist. | `blob/main/.github/SECURITY.md` |
| 59 | `no_leaked_credentials` (MUST NOT) | Met | gitleaks scans the full git history on every push and PR, plus a pre-commit gitleaks hook; no valid private credentials are committed. | `blob/main/.github/workflows/gitleaks.yml` |

## Analysis (8 criteria)

| # | Criterion (level) | Answer | Paste-ready justification | Evidence URL |
| --- | --- | --- | --- | --- |
| 60 | `static_analysis` (MUST) | Met | CodeQL performs static analysis of the Go code on every push, every pull request, and weekly; supplemented by golangci-lint, checkov, actionlint, and markdownlint in CI. | `blob/main/.github/workflows/codeql.yml` |
| 61 | `static_analysis_common_vulnerabilities` (SUGGESTED) | Met | CodeQL's Go security query pack checks for common vulnerability classes (CWE-mapped); results upload to GitHub code scanning. | `blob/main/.github/workflows/codeql.yml` |
| 62 | `static_analysis_fixed` (MUST) | Met | CodeQL findings are triaged through GitHub code scanning alerts; no medium or higher severity exploitable vulnerabilities are open. | `https://github.com/fini-net/gh-observer/security/code-scanning` |
| 63 | `static_analysis_often` (SUGGESTED) | Met | Static analysis runs on every push and pull request, plus weekly scheduled runs. | `blob/main/.github/workflows/codeql.yml` |
| 64 | `dynamic_analysis` (SUGGESTED) | Met | Seven native Go fuzz targets cover the security-critical parsers (PR URLs, Actions run URLs, repo arguments, timestamps, JSON number unmarshaling); seed corpora run in CI and full fuzzing is invocable via `just fuzz`. | `blob/main/justfile` |
| 65 | `dynamic_analysis_unsafe` (SUGGESTED, N/A) | N/A | The project is pure Go — memory-safe, no memory-unsafe languages. | `blob/main/go.mod` |
| 66 | `dynamic_analysis_enable_assertions` (SUGGESTED) | Met | Go test and fuzz builds enable extensive runtime checks (bounds checks, nil-dereference and type-assertion panics); fuzzing exercises the full parser surface with mutated inputs. | `blob/main/justfile` |
| 67 | `dynamic_analysis_fixed` (MUST, N/A) | Met | Fuzzing has found no exploitable vulnerabilities; fuzz seed corpora run in CI so regressions are caught, and any crashers are fixed before release. | `blob/main/.github/workflows/ci.yml` |

## Verify before you answer

These criteria make claims about recent activity that only you can confirm.
Spend five minutes on each before clicking the radio button — the badge is
self-certified, so honesty is the whole product:

- **13 `maintained`** — is there a release and reasonable commit activity
  within roughly the last year? (At time of writing: yes, comfortably.)
- **25 `report_responses` / 26 `enhancement_responses`** — scan the issue
  list for the last 2-12 months. Have most reports and requests gotten some
  reply, even if the reply was "no"? If honestly no, mark Unmet with a
  justification — a SHOULD criterion may be Unmet-with-justification
  without blocking the badge.
- **30 `vulnerability_report_response`** — N/A only holds if no one has
  reported a vulnerability in the last 6 months. If someone did, the initial
  response must have been within 14 days.
- **57 `vulnerabilities_fixed_60_days`** — confirm there is no publicly
  known (e.g., NVD-listed) vulnerability older than 60 days. With zero CVEs
  for this project, this should be quick.
- **62 `static_analysis_fixed`** — glance at the
  [code scanning alerts](https://github.com/fini-net/gh-observer/security/code-scanning)
  page and make sure it's actually clean before pasting that justification.
- **36 `test_most`** (optional) — `go test -cover ./...` gives you real
  numbers if you want them.

## Fix these before you submit

Two small repo blemishes that touch criteria in this form:

- ~~**Broken support link.**~~ Resolved: `.github/SUPPORT.md` was added in
  PR #476, so the `README.md` "Getting Support" link is now valid. This
  backs up criterion 2 (`interact`).
- **The zizmor claim.** CLAUDE.md says CI runs zizmor, and the repo carries
  `.github/zizmor.yml` plus inline `# zizmor: ignore` annotations — but no
  workflow in this repo actually *runs* zizmor. Either add a zizmor step to
  CI or stop telling people it runs. (The badge form never asks about
  zizmor, so this is hygiene, not a blocker — just do not cite it as
  evidence on the form.)

One more thing to keep in mind: **super-linter runs with
`continue-on-error: true`**, so it is advisory. Don't cite it anywhere on
the form; cite the gates that actually block merges (tests, CodeQL,
govulncheck, gitleaks, dependency-review, actionlint, markdownlint,
reviewdog).

## After you submit

- All six section counters should read full and the project page should flip
  to "in_progress" or "passing" depending on how you answered the SHOULD and
  SUGGESTED items.
- Embed the badge using the snippet the project page gives you
  (`[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12633/badge)](https://www.bestpractices.dev/projects/12633)`).
  The README already links the project; swapping in the live badge image is
  a nice touch once it turns green.
- The badge is a snapshot, not a one-time event. Revisit the form when
  circumstances change — most importantly, if a CVE is ever assigned
  (criterion 22 flips from N/A), if vulnerability reports start arriving
  (criterion 30 flips from N/A), or if maintenance goes quiet.

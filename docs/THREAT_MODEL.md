# Threat Model and Attack Surface Analysis

This document presents a systematic threat model and attack surface analysis
for gh-observer, satisfying the OpenSSF Best Practices requirement:

> When the project has made a release, the project MUST perform a threat
> modeling and attack surface analysis to understand and protect against
> attacks on critical code paths, functions, and interactions within the
> system.

It complements [DESIGN.md](DESIGN.md) (which documents all actors, actions,
and data flows) and [SECURITY.md](../.github/SECURITY.md) (which documents
security policies, release integrity, and credentials management).

## Methodology

This analysis uses the STRIDE threat classification applied across identified
trust boundaries. STRIDE categories:

| Category | Description |
| -------- | ----------- |
| **S** — Spoofing | Pretending to be something or someone else |
| **T** — Tampering | Unauthorized modification of data or code |
| **R** — Repudiation | Denying having performed an action |
| **I** — Information disclosure | Exposing data to unauthorized parties |
| **D** — Denial of service | Making a system or service unavailable |
| **E** — Elevation of privilege | Gaining capabilities beyond authorized level |

## System Context

gh-observer is a CLI tool that runs on a user's local machine. It authenticates
to GitHub's API using an OAuth token, polls for PR check run status, and
renders results in a terminal. It has no server component, no inbound network
listener, and no persistent storage beyond an optional debug log file.

The two execution modes—interactive TUI and non-interactive snapshot—share the
same code paths for authentication, API communication, and data parsing; they
differ only in output rendering.

## Trust Boundaries

A trust boundary is a line in the data flow diagram where data crosses from one
trust domain to another. Security assumptions may change at each boundary.

```text
┌─────────────────────────────────────────────────────────┐
│                    User's Machine                        │
│                                                         │
│  ┌──────────┐     ┌──────────────┐    ┌─────────────┐ │
│  │  Config   │────▶│              │    │  Debug Log   │ │
│  │  File     │ [1] │              │    │  File        │ │
│  └──────────┘     │              │    │ (if --debug) │ │
│                    │              │    └──────▲───────┘ │
│  ┌──────────┐     │              │           │[6]      │
│  │ CLI Args │────▶│              │           │         │
│  │ (PR/URL) │ [2] │  gh-observer │           │         │
│  └──────────┘     │   Process    │           │         │
│                    │              │    ┌──────┴───────┐ │
│  ┌──────────┐     │              │──▶ │  Terminal     │ │
│  │ `gh`     │────▶│              │[5] │  Display      │ │
│  │ Process  │ [3] │              │    └───────────────┘ │
│  └──────────┘     │              │                      │
│                    └──────┬───────┘                      │
│                          │[4]                           │
└──────────────────────────┼──────────────────────────────┘
                           │
                   HTTPS to api.github.com
                           │
                  ┌────────▼─────────┐
                  │   GitHub API      │
                  │   (REST + GraphQL)│
                  └──────────────────┘
```

### Trust boundary crossings

| ID | Boundary | Direction | Data crossing | Trust assumption |
| -- | -------- | --------- | ------------- | ---------------- |
| [1] | Config file → gh-observer | In | `refresh_interval`, `enable_links`, ANSI color codes | File is owned by the user; treated as trusted |
| [2] | CLI args → gh-observer | In | PR number, PR URL, `--quick`, `--debug` flags | User-supplied; validated by Cobra and regex |
| [3] | `gh` subprocess → gh-observer | In | Auth token, PR number/URL JSON | `gh` binary is on PATH; output is parsed as structured data |
| [4] | GitHub API ↔ gh-observer | Bidirectional | OAuth token (out), check run data + rate limits (in) | TLS protects in transit; API responses are treated as untrusted |
| [5] | gh-observer → terminal display | Out | PR titles, check names, annotations, URLs, hyperlinks | Terminal emulator processes escape sequences |
| [6] | gh-observer → debug log | Out | Error messages, API responses, potentially credential fragments | File permissions control access |

## Attack Surface

The attack surface is the set of entry points where an attacker can supply data
that the system processes.

| Entry point | Source | Untrusted? | Controls |
| ----------- | ------ | ---------- | -------- |
| CLI arguments | Local user / CI script | Partially (CI) | Cobra validation, regex for PR URLs |
| Config file (`~/.config/gh-observer/config.yaml`) | Local filesystem | Yes (if file tampered) | Viper defaults, type safety |
| `gh` subprocess output | Local process | Yes (compromised `gh`) | JSON parsing, cross-field validation |
| `GITHUB_TOKEN` env var | Environment | Yes (CI/shared hosts) | Used only as OAuth token |
| GitHub REST API responses | Network (TLS) | Yes (MITM / malicious Enterprise) | TLS certificate verification, `go-github` response parsing |
| GitHub GraphQL API responses | Network (TLS) | Yes (MITM / malicious Enterprise) | TLS, `githubv4` typed unmarshaling |
| Terminal display | N/A (output) | N/A | Lipgloss styling, termenv hyperlinks |

## Critical Code Paths

These are the code paths where security properties must be preserved. An
attack on any of these paths could compromise the confidentiality, integrity,
or availability of the system or the user's GitHub credentials.

### CP-1: Token acquisition and storage

**Files:** `internal/github/client.go`

The application obtains a GitHub OAuth token from either `GITHUB_TOKEN` or the
`gh auth token` subprocess. The token is held in memory as a Go string and
passed to `oauth2.StaticTokenSource`. It is never written to persistent
storage by the application itself.

**Risk:** The debug logger at `client.go:27` logs the combined stdout+stderr
output of `gh auth token` when that command fails. In some failure modes, `gh`
may emit the token or a portion of it on stderr, which would be written to the
debug log file. Debug log files are created with default umask permissions
(typically `0644`) in a directory with `0755` permissions, making them
readable by other local users.

**Mitigations:**

- Debug logging is opt-in (`--debug` flag); not enabled by default
- The token itself is never passed to `debug.Log()`
- Log directory is under `os.TempDir()`, which is typically per-user on modern
  systems

**Accepted risk:** The `gh auth token` failure-path logging is a
defense-in-depth gap. A fix to redact or omit the `output` field from that log
entry would further reduce the attack surface, but the practical risk is limited
because (a) debug mode requires explicit opt-in, (b) the `gh` binary rarely
emits tokens on stderr, and (c) the attacker needs local access to read the log
file.

### CP-2: Subprocess execution

**Files:** `internal/github/client.go`, `internal/github/pr.go`

The application executes `gh` as a subprocess for token retrieval and PR
detection. The commands are:

- `gh auth token` — no user-supplied arguments
- `gh pr view --json number,url` — no user-supplied arguments
- `git remote get-url origin` — no user-supplied arguments

No CLI arguments (PR number, URL, flags) are passed to any subprocess. All
subprocess invocations use fixed argument lists.

**Risk:** A compromised `gh` binary on the PATH could execute arbitrary code
with the user's privileges. This is a standard PATH-based attack vector common
to all tools that invoke `gh`.

**Mitigations:**

- No user-supplied arguments flow to subprocess commands
- The application validates all subprocess output (JSON parsing, regex
  validation, cross-field consistency checks)
- `ParsePRURL` re-validates the URL returned by `gh pr view` against an
  anchored regex (`^https?://github\.com/...`)
- PR numbers from `gh` output are cross-checked against the URL

**Accepted risk:** PATH-based substitution is outside the scope of this
application. Users must ensure their PATH is trustworthy, as with any tool that
invokes `gh` or `git`.

### CP-3: GitHub API response parsing

**Files:** `internal/github/graphql.go`, `internal/github/client.go`,
`internal/github/history.go`, `internal/github/checks.go`

API responses from GitHub (REST and GraphQL) are the primary data source for
the application. All responses are parsed by well-maintained libraries
(`go-github`, `githubv4`) into typed Go structs. The application does not
perform eval-like operations on API data.

**Risk:** A man-in-the-middle on the GitHub API (requiring a TLS certificate
compromise or malicious corporate proxy) could inject crafted data into API
responses. This data flows into:

- Terminal display as strings (PR titles, check names, annotations, error
  messages)
- OSC 8 hyperlink generation (check `DetailsURL` values)
- Rate limit values that control polling backoff
- Timestamp strings that are parsed with `ParseTimestamp`

**Mitigations:**

- TLS with certificate verification protects all API communication in transit
- Typed struct unmarshaling prevents injection of unexpected data types
- No API-sourced data is used in code execution, file I/O (beyond debug
  logging), or network connections to arbitrary hosts
- Rate limit manipulation can only cause nuisance effects (excessive polling
  or delayed display)—no data security impact

**Accepted risk:** Terminal escape sequence injection from API-sourced
strings is theoretically possible but requires a TLS-level MITM. The
practical risk is negligible. See TV-5 through TV-8 below for details.

### CP-4: Configuration file loading

**Files:** `internal/config/config.go`

The config file at `~/.config/gh-observer/config.yaml` is loaded via Viper.
Values are type-safe (Go struct fields). Missing or partial configs fall back
to sensible defaults.

**Risk:** An attacker with write access to the config file could:

- Set `refresh_interval` to an extremely low value (e.g., `1ns`), causing
  rapid API polling and rate limit exhaustion
- Set `enable_links` to `true` (already the default) to enable hyperlink
  rendering
- Change ANSI color codes, which could theoretically construct escape
  sequences (limited by Viper's type system to integers)

**Mitigations:**

- Config file requires local filesystem write access (same trust level as the
  user's shell profile, SSH config, etc.)
- Viper type safety prevents injection of non-integer values into color fields
- `refresh_interval` is a `time.Duration`; extremely low values cause excessive
  API calls but no data compromise

**Accepted risk:** Config file tampering is equivalent to any local privilege
escalation. If an attacker can write to the user's config directory, they can
compromise far more sensitive targets than gh-observer.

### CP-5: PR URL parsing and validation

**Files:** `internal/github/pr.go`

CLI positional arguments (PR number or URL) are parsed by Cobra and validated
before use. `ParsePRURL` uses a fully-anchored regex that only accepts
`https?://github.com/([^/]+)/([^/]+)/pull/(\d+)`.

**Risk:** A crafted URL could bypass the regex to extract unexpected
owner/repo values.

**Mitigations:**

- Fully anchored regex (`^` and `$`) prevents prefix/suffix attacks
- Only `github.com` host is accepted—no open redirect or SSRF
- Extracted owner/repo values are passed to `go-github` library calls, which
  URL-encode path components
- PR numbers are parsed as integers via `strconv.Atoi`
- `parsePRViewWithRepo` cross-checks the PR number from `gh` output against
  the URL

**Accepted risk:** None—validation is thorough.

### CP-6: Terminal output rendering

**Files:** `internal/tui/view.go`, `internal/tui/display.go`, `main.go`

PR titles, check names, check summaries, annotation messages, and
`DetailsURL` values are rendered to the terminal without explicit ANSI escape
sequence sanitization. Lipgloss applies styling by wrapping strings with ANSI
sequences but does not strip embedded escape sequences from input.

**Risk:** API-sourced strings containing ANSI escape sequences or OSC commands
could alter terminal behavior (title bar changes, clipboard writes, cursor
manipulation) or break the TUI layout.

**Mitigations:**

- Data source is GitHub's API, which constrains check names and annotation text
  to YAML-configured values
- The user explicitly chooses which PR to watch
- Snapshot mode (non-TTY) produces plain text output with limited escape
  sequence impact
- `termenv.Hyperlink` uses standard OSC 8 formatting

**Accepted risk:** Terminal injection requires either a MITM on the GitHub API
or a malicious GitHub Enterprise instance injecting crafted check names.
This is a very low-probability scenario for a CLI tool that users deliberately
point at repos they control.

## STRIDE Analysis

### TB-1: User's Machine → gh-observer (via CLI arguments)

| ID | Threat | Category | Impact | Mitigation | Status |
| -- | ------ | -------- | ------ | ---------- | ------ |
| TV-1 | Attacker crafts a PR URL pointing to a malicious host | Spoofing | gh-observer sends API requests (with auth token) to an unintended owner/repo | `ParsePRURL` only accepts `github.com`; extracted values are used in API calls to `api.github.com`, not arbitrary hosts | **Mitigated** |
| TV-2 | Attacker supplies a PR URL with path traversal characters | Tampering | Could cause unexpected API paths | `go-github` library URL-encodes path components; API returns 404 for invalid paths | **Mitigated** |
| TV-3 | Attacker supplies an extremely large PR number | DoS | `strconv.Atoi` converts it to an int; API call fails with 404 | No resource exhaustion possible; Go's int overflow wraps to negative, failing API validation | **Mitigated** |

### TB-2: Config File → gh-observer

| ID | Threat | Category | Impact | Mitigation | Status |
| -- | ------ | -------- | ------ | ---------- | ------ |
| TV-4 | Attacker sets `refresh_interval` to 1ns in config | DoS | Rapid API polling exhausts GitHub rate limit for the user's token | Requires local write access; effect is limited to rate limit exhaustion (not data compromise); user can delete the config file | **Accepted**—local file write implies broader compromise |
| TV-5 | Attacker injects ANSI escapes via color code integers | Information disclosure / Tampering | Viper reads color values as integers; ANSI 256-color codes are valid only in the range 0–255 and are consumed by lipgloss, not emitted raw | Viper type enforcement; lipgloss validates color values | **Mitigated** |

### TB-3: `gh` Subprocess → gh-observer

| ID | Threat | Category | Impact | Mitigation | Status |
| -- | ------ | -------- | ------ | ---------- | ------ |
| TV-6 | Compromised `gh` binary returns a spoofed auth token | Spoofing | gh-observer uses a token controlled by the attacker, sending API requests through the attacker's proxy | The token is used for GitHub API calls only; the attacker gains no capabilities beyond reading the same public/private repos; TLS certificate pinning in Go's HTTP client prevents arbitrary MITM | **Accepted**—PATH security is outside this application's scope |
| TV-7 | Compromised `gh` binary returns a spoofed PR URL | Spoofing | gh-observer watches a different PR than intended | `ParsePRURL` re-validates the URL; cross-check between PR number and URL fields detects inconsistency | **Mitigated** |

### TB-4: GitHub API → gh-observer (REST and GraphQL)

| ID | Threat | Category | Impact | Mitigation | Status |
| -- | ------ | -------- | ------ | ---------- | ------ |
| TV-8 | MITM injects ANSI escape sequences in PR title or check names | Tampering / Information disclosure | Terminal behavior altered (title bar, clipboard, cursor) | TLS in transit; GitHub sanitizes check names; user explicitly chose the PR | **Mitigated** (defense in depth) |
| TV-9 | MITM injects crafted rate limit values | DoS | High values prevent backoff (exhausting real rate limit); low values cause excessive backoff (stale display) | Effect is limited to polling frequency; no data security impact; user can restart the tool | **Accepted**—nuisance only |
| TV-10 | MITM injects crafted `DetailsURL` to break OSC 8 hyperlink format | Tampering | Arbitrary OSC sequences could be emitted | `termenv.Hyperlink` uses standard terminators; data source is GitHub API (TLS-protected); URLs are GitHub canonical paths | **Mitigated** (defense in depth) |
| TV-11 | MITM injects malformed timestamps to cause panic or infinite loop | DoS | `ParseTimestamp` returns an error; the application logs and skips the value | Error-handling path is tested; no panic or infinite loop possible | **Mitigated** |
| TV-12 | MITM injects extremely long strings in API responses | DoS | Large strings consume memory during rendering | Go's garbage collector reclaims strings after rendering; no persistent storage of API response data | **Accepted**—memory is bounded by API response size |

### TB-5: gh-observer → Terminal Display

| ID | Threat | Category | Impact | Mitigation | Status |
| -- | ------ | -------- | ------ | ---------- | ------ |
| TV-13 | Terminal emulator processes injected ANSI/OSC sequences | Information disclosure / Tampering | Title bar changes, clipboard writes, cursor manipulation, visual spoofing | Data source is GitHub API (TLS-protected); user chose the PR; practical exploitation requires MITM | **Accepted**—terminal trust model is standard for TUI applications |

### TB-6: gh-observer → Debug Log File

| ID | Threat | Category | Impact | Mitigation | Status |
| -- | ------ | -------- | ------ | ---------- | ------ |
| TV-14 | Debug log contains credential fragments from `gh auth token` failure | Information disclosure | Other local users read the token from the debug log file | Opt-in debug mode (`--debug`); `gh` rarely emits tokens on stderr; attacker needs local filesystem access | **Partially mitigated**—recommend redacting the `output` field in `client.go:27` |
| TV-15 | Debug log file has world-readable permissions | Information disclosure | Any data in the log is readable by other local users | Default umask is local policy; `os.TempDir()` is often per-user on modern systems | **Partially mitigated**—recommend restricting file permissions to `0600` |

## Threat-to-Mitigation Summary

| Threat ID | Category | Mitigation status | Action items |
| --------- | -------- | ----------------- | ------------ |
| TV-1 | Spoofing | Mitigated | None |
| TV-2 | Tampering | Mitigated | None |
| TV-3 | DoS | Mitigated | None |
| TV-4 | DoS | Accepted | Local file write implies broader compromise |
| TV-5 | Info disclosure | Mitigated | None |
| TV-6 | Spoofing | Accepted | PATH security outside scope |
| TV-7 | Spoofing | Mitigated | None |
| TV-8 | Tampering | Mitigated | Defense in depth—consider adding ANSI sanitization to API-sourced strings |
| TV-9 | DoS | Accepted | Nuisance only; no data security impact |
| TV-10 | Tampering | Mitigated | Defense in depth |
| TV-11 | DoS | Mitigated | None |
| TV-12 | DoS | Accepted | API response size is the natural bound |
| TV-13 | Info disclosure | Accepted | Standard terminal trust model |
| TV-14 | Info disclosure | Partially mitigated | Redact `output` field from `client.go:27` debug log |
| TV-15 | Info disclosure | Partially mitigated | Set debug log file permissions to `0600` |

## Security Properties

Based on this analysis, gh-observer maintains the following security properties:

1. **Token confidentiality:** The OAuth token is held in memory only and is
   not written to persistent storage by the application (with the partial
   exception noted in TV-14 when debug mode is enabled and `gh auth token`
   fails).

2. **No inbound attack surface:** The application has no network listener, no
   IPC endpoint, and no signal handler that processes external input beyond the
   standard OS signals for termination.

3. **No arbitrary code execution from API data:** API responses flow only into
   display rendering and time-duration calculations. No `eval`, `exec`,
   `os.Open`, or `net.Dial` operates on API-sourced data.

4. **No file modification (except debug logs):** The application does not
   create, modify, or delete any files on the user's system unless `--debug`
   is enabled.

5. **Network egress restricted to GitHub:** All outbound connections go to
   `api.github.com` or `github.com` over HTTPS. No connections to arbitrary
   hosts.

6. **No user-supplied data in subprocess calls:** No CLI arguments, API
   responses, or config values are passed to `exec.Command` arguments. The
   `gh` subprocess invocations use fixed argument lists only.

## Recommended Improvements

These improvements would further harden the application beyond its current
state:

1. **Redact `gh auth token` output in debug logs** (`client.go:27`): Replace
   the `"output"` field with a redacted version or omit it entirely. This
   eliminates the TV-14 risk.

2. **Restrict debug log file permissions** (`logger.go`): Create the debug log
   file with `0600` permissions instead of relying on the default umask. This
   addresses TV-15.

3. **Sanitize API-sourced strings for terminal rendering:** Strip ANSI/OSC
   escape sequences from PR titles, check names, annotations, and URLs before
   rendering. This adds a defense-in-depth layer for TV-8 and TV-10.

4. **Bound `refresh_interval` config value:** Reject or clamp values below a
   reasonable minimum (e.g., 1 second) to prevent the TV-4 nuisance scenario.

These improvements are not blocking for release—their risks are accepted or
mitigated by other controls—but they would reduce the attack surface
incrementally.

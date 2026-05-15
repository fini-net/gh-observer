# gh observer

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/fini-net/gh-observer/badge)](https://scorecard.dev/viewer/?uri=github.com/fini-net/gh-observer)
[![OpenSSF Baseline](https://www.bestpractices.dev/projects/12633/baseline)](https://www.bestpractices.dev/projects/12633)
![GitHub Issues](https://img.shields.io/github/issues/fini-net/gh-observer)
![GitHub Pull Requests](https://img.shields.io/github/issues-pr/fini-net/gh-observer)
![GitHub License](https://img.shields.io/github/license/fini-net/gh-observer)
![GitHub watchers](https://img.shields.io/github/watchers/fini-net/gh-observer)

A GitHub PR check and Actions run watcher that improves on `gh pr checks --watch`
by showing runtime metrics, queue latency, and better handling of startup delays.

![project banner: abstract representation of code flowing through a pull request, using interconnected nodes and lines.](docs/gh-observer-banner.jpeg)

## Why?

The existing `gh pr checks --watch` doesn't show how long checks have been
running, doesn't handle the 30-90s startup delay well, and doesn't show queue
latency. This creates anxiety when watching CI runs - "is it stuck or just
slow?"

## Features

- ⏱️ **Runtime metrics** - Shows elapsed time: `3m 52s` tells you exactly how
  long checks have been running
- ⏳ **Queue latency** - Displays wait time: `15s` shows how long GitHub queued
  the job before starting
- 🔄 **Real-time updates** - Auto-refreshes every 5s (configurable) without
  manual polling
- ⚡ **Startup phases** - Helpful messages like "Waiting for Actions to
  start..." during the 30-90s GitHub delay
- 🔧 **Actions run watching** - Monitor any GitHub Actions workflow run by
  URL, not just PR checks
- 🛡️ **Rate limits** - Backs off automatically when approaching API limits to
  avoid interruptions
- 📊 **Historical averages** - Shows average runtime for each job based on
  recent completed runs, so you know if things are taking longer than usual
- ⚡ **`--quick` mode** - Skip the historical averages fetch when you just want
  a fast snapshot
- ✅ **CI-friendly** - Returns exit codes (0=success, 1=failure) for script
  automation

## Example Output

```ShellOutput
PR #123: Add new feature

Startup Phase (37s elapsed):
  ⏳ Waiting for Actions to start...
  💡 GitHub typically takes 30-90s to queue jobs after PR creation

PR #5: 🔶 [claude] /init 21:04:15 UTC
Updated 0s ago  •  Pushed 43h 8m 11s ago

Startup   Workflow/Job                                Duration   Avg

  15s ✗ MarkdownLint / lint                             5s        6s
   .github:13 - Failed with exit code: 1
   CLAUDE.md:100 - Lists should be surrounded by blank lines: CLAUDE.md:100 MD032/blanks-around-lists Lists should be surr

  15s ✓ Auto Assign / run                               5s        4s
  15s ✓ CUE Validation / verify                         6s        7s
  15s ✓ Checkov / scan                                 27s       31s
  15s ✓ Claude Code Review / claude-review          3m 52s   4m 10s
  15s ✓ Lint GitHub Actions workflows / actionlint      8s        9s
  39s ✓ Checkov                                         2s        2s

Press q to quit
```

## Example Animations

Thanks to [asciinema](https://asciinema.org/) we can show you how our extension
looks in practice.  You can compare to [old animations](docs/OldAnimations.md) to
see how we have evolved.

### PR that was already merged

![animation of checking the GHA status on a merged PR](docs/gh-observer-merged-pr2.gif)

This shows in real-time (not accelerated) how it goes when you check out a
PR that is already merged.

### PR in remote repo that was already merged

Running `gh observer https://github.com/MartinDelille/nautilus/pull/13` results in:

![animation of checking the GHA status on a merged PR in a remote repo](docs/gh-observer-uncloned-repo.gif)

Again in real-time, but now checking out a PR on a repo that we do not
have cloned locally.  Note: it requires the full URL.

### PR that was just created

![animation of watching checks after creating a PR](docs/gh-observer-active-pr.gif)

This was sped up 2x.  In this example, the Claude check fails, illustrating how
error log output is integrated alongside the list of jobs.

### PR that was just created with GHAs that use descriptions

![animation of watching checks after creating a PR with GHAs that use descriptions](docs/gh-observer-descriptions2.gif)

The [Super-Linter](https://github.com/super-linter/super-linter) and a few other
GitHub Actions utilize the description field to convey success or failure.  Our
extension doesn't show descriptions for successful checks and displays them for
cases with errors to be consistent with the GitHub Actions that make it easier
to show the right bit of the logs.  Since we don't try to show the logs for
super-linter, you're "stuck" clicking on the title of the job in your terminal
and it will open up in your favorite web browser.

This was sped up 10x.

## Installation

### Install precompiled binary

The easiest way to install gh-observer is as a GitHub CLI extension:

```bash
gh extension install fini-net/gh-observer
```

This installs a precompiled binary for your platform - no Go toolchain required. To install a specific version:

```bash
gh extension install fini-net/gh-observer --pin v1.0.0
```

### Or install via go install

If you prefer installing via Go:

```bash
go install github.com/fini-net/gh-observer@latest
```

### Or build from source

To build from source:

```bash
git clone https://github.com/fini-net/gh-observer.git
cd gh-observer
just build
```

## Usage

### Auto-detect PR from current branch

```bash
gh observer
```

### Watch specific PR in current repo

```bash
gh observer 123
```

### Watch external PR by URL

You can watch checks on any public PR by passing the full URL - no need to clone the repo:

```bash
gh observer https://github.com/StackExchange/dnscontrol/pull/3941
```

This works for any GitHub PR URL and is useful for:

- Reviewing checks on a colleague's PR in another repo
- Monitoring upstream dependencies before merging
- Following CI status on projects you don't have cloned locally

### Watch an Actions workflow run

You can also watch any GitHub Actions workflow run by passing its URL. This is
useful for monitoring CI triggered by a merge to main, a scheduled workflow,
or a `workflow_dispatch` event - things that aren't tied to a PR:

```bash
gh observer https://github.com/fini-net/gh-observer/actions/runs/25856656092
```

The display shows each job in the run with runtime metrics and historical
averages, just like PR mode but without the queue latency column:

```ShellOutput
fini-net/gh-observer: CI  15:04:05 UTC
Updated 5s ago  •  Pushed 2m ago

    Workflow/Job                                ThisRun   HistAvg

✓ CI / test                                      1m 30s    1m 25s
◐ CI / lint                                        45s        --
✗ CI / deploy                                    2m 10s    1m 50s

Press q to quit
```

The header shows the repo name, the run's display title, and how long ago
the head commit was pushed (or when the run was created if commit info is
unavailable). Exit code follows the same convention: 0 if all jobs succeed,
1 if any job fails.

### Skip historical averages for a faster snapshot

If you just want a quick look without waiting for the historical averages
fetch, use `--quick` (or `-q`):

```bash
gh observer --quick
gh observer -q 123
```

This skips the extra API calls for historical job runtimes and prints
immediately. Useful when you're in a hurry or don't have the API budget
to spare.

### Use in CI pipelines

Our primary focus is on improving the interactive experience, but we also
set the `exit` code for the process in a potentially useful way.

```bash
# Wait for PR checks to complete and exit with their status
gh observer && echo "All checks passed!"

# Wait for an Actions run to complete and exit with job status
gh observer https://github.com/owner/repo/actions/runs/123456789 && echo "All jobs passed!"
```

## Configuration

Create `~/.config/gh-observer/config.yaml` to customize settings:

```yaml
# Refresh interval for polling GitHub API
refresh_interval: 5s

# Color codes for terminal output (ANSI 256-color palette)
colors:
  success: 10  # Green - completed successfully
  failure: 9   # Red - completed with failure
  running: 11  # Yellow - currently in progress
  queued: 8    # Gray - waiting to start
```

See `.config.example.yaml` for reference.

## Authentication

gh-observer uses GitHub authentication in this order:

1. `GITHUB_TOKEN` environment variable
2. `gh` CLI authentication (`gh auth token`)

Make sure you have either set up.

## Supported Platforms

Precompiled binaries are available for:

- **macOS**: Intel (amd64) and Apple Silicon (arm64)
- **Linux**: x86-64 (amd64) and ARM64
- **Windows**: x86-64 (amd64)

All binaries include supply chain security attestations for verification.

## Verifying Release Assets

Every release includes three independent mechanisms for verifying the integrity
and authenticity of downloaded binaries. You can use any combination of these
methods.

### Option 1: GitHub Build Attestations (recommended)

This is the simplest method and verifies that the binary was built by our
release workflow on GitHub's infrastructure:

```bash
gh attestation verify darwin-arm64 --owner fini-net
```

Replace `darwin-arm64` with the binary matching your platform. Available
binaries: `darwin-amd64`, `darwin-arm64`, `linux-amd64`, `linux-arm64`,
`freebsd-amd64`, `freebsd-arm64`, `windows-amd64.exe`, `windows-arm64.exe`.

### Option 2: Cosign Signatures (keyless)

Each binary is signed using Sigstore keyless (certificate-based) signing. To
verify a binary against its signature and certificate:

```bash
cosign verify-blob darwin-arm64 \
  --certificate darwin-arm64.pem \
  --signature darwin-arm64.sig \
  --certificate-identity https://github.com/fini-net/gh-observer/.github/workflows/release.yml@refs/tags/v1.0.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Alternatively, you can verify using the cosign bundle file:

```bash
cosign verify-blob darwin-arm64 \
  --bundle darwin-arm64.bundle \
  --certificate-identity https://github.com/fini-net/gh-observer/.github/workflows/release.yml@refs/tags/v1.0.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Replace `v1.0.0` with the release version you are verifying. The `.sig`,
`.pem`, and `.bundle` files are available on the
[releases page](https://github.com/fini-net/gh-observer/releases).

### Option 3: SLSA Provenance

Each release includes a `.intoto.jsonl` provenance attestation generated by the
[SLSA GitHub Generator](https://github.com/slsa-framework/slsa-github-generator),
providing SLSA Build Level 3 guarantees. This attestation is non-forgeable proof
of the build origin and can be verified using the
`slsa-verifier` tool:

```bash
slsa-verifier verify-artifact darwin-arm64 \
  --provenance-path gh-observer.intoto.jsonl \
  --source-uri github.com/fini-net/gh-observer \
  --builder-id https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml
```

### Verifying Checksums Manually

You can also verify SHA-256 checksums directly. Download the checksums from the
release assets and compare:

```bash
sha256sum -c checksums.txt
```

### Verifying the Release Author

To verify the identity of the person or process that authored the release:

1. **Check the release tag commit** - View the author and DCO signature on the
   commit the release tag points to:

   ```bash
   git log -1 --format='Author: %an <%ae>%nSigned-off-by: %(trailers:key=Signed-off-by)' v1.8.0
   ```

2. **Check the GitHub release author** - Each release on the
   [releases page](https://github.com/fini-net/gh-observer/releases) shows the
   GitHub username of the maintainer who created it.

3. **Verify build attestations** - Confirms the binary was produced by the
   authorized release workflow (see Option 1 above).

See [Verifying Release Author Identity](.github/SECURITY.md#verifying-release-author-identity)
in the security policy for full details.

## Known Limitation: Live Logs for Slow Jobs

We'd love to show you live (non-error) logs while your jobs are still running —
especially for jobs that take over a minute. Unfortunately, the
[GitHub Actions API no longer returns logs in real-time](https://github.com/orgs/community/discussions/154834).
The logs endpoint returns a 404 while a job is in progress and only serves logs
after the job completes. This makes streaming or tailing job logs via the API
impossible today.

Multiple attempts to work around this (see
[#127](https://github.com/fini-net/gh-observer/issues/127)) have confirmed
there is no viable alternative. GitLab supports this; GitHub does not (yet).

If live job logs would help your workflow, please
[upvote and comment on the community discussion](https://github.com/orgs/community/discussions/154834)
to let GitHub know this matters. The more voices, the more likely we'll get a
real-time log streaming API.

## Testing

All major changes (including new features, bug fixes, and behavior changes)
must add or update tests that verify the changed functionality. Tests run
automatically in CI on every push to `main` and on every pull request (via the
[CI workflow](.github/workflows/ci.yml)). They also run locally before creating
a PR (via the `just pr` hook).

### Running tests locally

```bash
# Run all unit tests
just test
# Or equivalently
go test ./...

# Run a specific package
go test ./internal/timing/...
go test ./internal/github/...
go test ./internal/tui/...
go test ./internal/config/...
go test ./internal/debug/...
```

### What the tests cover

Unit tests cover timing calculations (queue latency, runtime, duration
formatting), GitHub API parsing (GraphQL check runs, REST workflow runs,
history fetching, PR URL and Actions run URL parsing), TUI logic (display
formatting, state updates, exit codes for both PR and run modes),
configuration loading, and debug logging. TUI rendering and live GitHub API
interactions are tested manually by running `just build` and pointing
the binary at a real PR or Actions run URL.

## Development

This project uses [just](https://github.com/casey/just) for task automation:

```bash
# Build the binary and install locally as gh extension
just build

# Run on current PR
gh observer

# Create a PR
just pr

# Merge a PR
just merge
```

## Architecture

Built with:

- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [go-github](https://github.com/google/go-github) - GitHub API client
- [Viper](https://github.com/spf13/viper) - Configuration management

See `CLAUDE.md` for detailed implementation notes.  Read the [linear walkthrough](docs/linear-walkthrough.md)
to get a detailed walkthrough of the code and to learn why some
of the design choices were made.  See [Design Documentation](docs/DESIGN.md)
for a comprehensive description of all actors, actions, and data flows
within the system.

## Contributing

- [Code of Conduct](.github/CODE_OF_CONDUCT.md)
- [Contributing Guide](.github/CONTRIBUTING.md) includes a step-by-step guide to our
  [development process](.github/CONTRIBUTING.md#development-process).

## Support & Security

- [Getting Support](.github/SUPPORT.md)
- [Security Policy](.github/SECURITY.md) (including [release support scope and end-of-life policy](.github/SECURITY.md#release-support))

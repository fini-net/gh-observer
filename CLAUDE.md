# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

gh-observer is a GitHub PR check watcher CLI tool that improves on `gh pr checks --watch` by showing runtime metrics, queue latency, and better handling of startup delays. Built as a Go application with a TUI (Terminal User Interface) using Bubbletea.

## Development Workflow

This repo uses `just` for all development tasks:

### Common development commands

- `just build` - Build the `gh-observer` binary and install locally as gh extension
- `just branch <name>` - Create a new feature branch (format: `$USER/YYYY-MM-DD-<name>`)
- `just pr` - Create PR, push changes, and watch checks
- `just again` - Push changes, update PR description, and watch GHAs (most common iterative workflow)
- `just merge` - Squash merge PR, delete branch, return to main, and pull latest
- `just sync` - Return to main branch, pull latest, and check status

### Other useful commands

- `just prweb` - Open current PR in web browser
- `just pr_update` - Update PR description with current commits (done automatically by `again`)
- `just pr_checks` - Watch GHAs then check for Copilot suggestions
- `just claude_review` - Claude's latest PR code review
- `just pr_verify` - Add or append to Verify section from stdin
- `just deps-update` - Update Go dependencies and tidy go.mod/go.sum
- `just test2cast <pr>` - Record asciinema demo of watching a specific PR
- `just release_status` - Check release workflow status and list binaries
- `just release_age` - Check how long ago the last release was

### Testing

- `just test` or `go test ./...` - Run all unit tests
- `go test ./internal/timing/...` - Run timing tests only
- `go test ./internal/github/...` - Run GitHub client tests only

Unit tests cover timing calculations, log parsing, history fetching, PR parsing, and TUI display/update logic. The TUI rendering and live GitHub API interactions are tested manually by running `just build` and pointing the binary at a real PR.

## Release Workflow

gh-observer uses automated binary building for releases via GitHub Actions and `gh-extension-precompile`.

### Creating a Release

To create a new release:

```bash
just release v1.0.0
```

This command:

1. Runs `gh release create v1.0.0 --generate-notes`
2. Creates the GitHub release with auto-generated release notes
3. Pushes the version tag to the repository
4. Triggers `.github/workflows/release.yml` via the tag push

### Automated Binary Building

When a version tag (matching `v*`) is pushed, the release workflow automatically:

1. **Builds cross-platform binaries** for 5 platforms:
   - `gh-observer_<version>_darwin-amd64` - macOS Intel
   - `gh-observer_<version>_darwin-arm64` - macOS Apple Silicon
   - `gh-observer_<version>_linux-amd64` - Linux x86-64
   - `gh-observer_<version>_linux-arm64` - Linux ARM64
   - `gh-observer_<version>_windows-amd64.exe` - Windows

2. **Generates checksums** (`gh-observer_<version>_checksums.txt`)

3. **Creates build attestations** for supply chain security (verifiable via `gh attestation verify`)

4. **Attaches all artifacts** to the GitHub release

### Workflow Architecture

The `.github/workflows/release.yml` workflow:

- **Trigger**: Push of tags matching `v*` pattern
- **Action**: Uses `cli/gh-extension-precompile@v2`
- **Go Version**: Auto-detected from `go.mod` (currently 1.26.2) via `go_version_file` parameter
- **Security**: Generates attestations with `generate_attestations: true`
- **Permissions**: Requires `contents: write`, `id-token: write`, `attestations: write`

### Testing with Prereleases

To test the release workflow without committing to a stable version:

```bash
# Create a prerelease tag (tags with hyphens create prereleases automatically)
git tag v0.1.0-rc.1
git push origin v0.1.0-rc.1

# Watch the workflow run
gh run watch

# Verify release assets were created
gh release view v0.1.0-rc.1

# Test installation
gh extension install fini-net/gh-observer

# Verify build attestation (macOS example)
gh attestation verify gh-observer_v0.1.0-rc.1_darwin-arm64 --owner fini-net
```

### Post-Release Verification

After running `just release`, you can verify the workflow completed successfully:

```bash
# Check workflow status
just release_status

# Or manually verify
gh release view v1.0.0
gh run list --workflow=release.yml --limit 5
```

### Installation for End Users

Once released, users can install without the Go toolchain:

```bash
# Install latest version
gh extension install fini-net/gh-observer

# Install specific version
gh extension install fini-net/gh-observer --pin v1.0.0

# Upgrade to latest
gh extension upgrade gh-observer
```

### Supply Chain Security

All release binaries include build attestations:

- Generated via GitHub Actions' built-in attestation feature
- Provides cryptographic proof of build provenance
- Can be verified using `gh attestation verify <binary> --owner fini-net`
- Attestation files are automatically attached to releases

### Design Decisions

- **No GPG signing**: Build attestations provide equivalent security without secret management complexity
- **Automatic Go version detection**: Uses `go_version_file: go.mod` to stay in sync with project requirements
- **Standard build process**: No custom build scripts needed - `go build` works perfectly for this project
- **Complementary workflows**: The `just release` command creates releases; the GitHub Action builds binaries. They work together, not as replacements.

## Architecture

### High-level structure

gh-observer follows a clean architecture with distinct layers:

1. **Main entry point** (`main.go`) - Handles command-line arguments using Cobra, configuration loading, and mode selection (TUI vs snapshot)
2. **GitHub client layer** (`internal/github/`) - Abstracts GitHub API interactions
3. **TUI layer** (`internal/tui/`) - Implements Bubbletea model/view/update pattern for interactive mode
4. **Configuration** (`internal/config/`) - Loads user config from `~/.config/gh-observer/config.yaml`
5. **Timing utilities** (`internal/timing/`) - Calculates queue latency, runtime, and formats durations

### Execution modes

The application operates in two modes based on whether stdout is a terminal:

**Interactive mode** (default when running in a terminal):

- Uses Bubbletea TUI with live updates
- Polls GitHub API every 5s (configurable)
- Shows spinner and real-time status changes
- Automatically quits when all checks complete
- Supports keyboard input (q to quit)

**Snapshot mode** (when stdout is not a terminal, e.g., in scripts or CI):

- Implemented in `runSnapshot()` function in `main.go`
- Prints a single snapshot of current check status
- Plain text output without colors or TUI
- Exits immediately after printing
- Returns appropriate exit code based on check results
- Useful for scripting: `gh-observer && echo "All checks passed!"`

### Bubbletea TUI architecture

The TUI follows the Elm Architecture pattern (Model-View-Update):

- **Model** (`internal/tui/model.go`) - Application state including PR metadata, check runs, rate limits, and UI state
- **Init** (`internal/tui/update.go`) - Initializes the model and kicks off PR info fetch
- **Update** (`internal/tui/update.go`) - Message handler that processes:
  - `TickMsg` - Periodic refresh (every 5s configurable)
  - `PRInfoMsg` - PR metadata (title, SHA, timestamps)
  - `ChecksUpdateMsg` - Check run status updates
  - `WorkflowsDiscoveredMsg` - Workflow discovery results (run→workflow ID mappings)
  - `JobAveragesPartialMsg` - Per-workflow history fetch results (averages for one workflow)
  - `tea.KeyMsg` - Keyboard input (q to quit)
- **View** (`internal/tui/view.go`) - Renders the terminal UI
- **Display** (`internal/tui/display.go`) - Column formatting, alignment widths, URL building for check hyperlinks
- **Styles** (`internal/tui/styles.go`) - Lipgloss color scheme and styling
- **Messages** (`internal/tui/messages.go`) - Custom message types for async operations
- **Constants** (`internal/tui/constants.go`) - Tuning thresholds: `slowJobThreshold` (2m), `verySlowJobThreshold` (3m), `rateBackoffThreshold` (10), `minRateLimitForFetch` (100), `historyFetchDelay` (10s)

### GraphQL architecture

The `internal/github/graphql.go` module uses GraphQL to efficiently fetch check run data:

**Query structure** - Follows `gh pr checks` pattern:

```ShellOutput
Repository → PullRequest → Commits → StatusCheckRollup → CheckRun
  → CheckSuite → WorkflowRun → Workflow → Name
```

**Key benefits**:

- Single API call gets all data (workflow name + check status)
- More efficient than REST API (fewer API calls, less rate limit usage)
- Returns enriched `CheckRunInfo` with workflow name included
- Supports cursor-based pagination for PRs with more than 100 status contexts

**Display format** - Check names shown as "Workflow Name / Job Name":

- "CUE Validation / verify"
- "MarkdownLint / lint"
- "Claude Code Review / claude-review"
- "Checkov" (legacy checks without workflow show job name only)

**Error annotations** - Failed checks display inline error details:

- `CheckRunInfo` includes `Summary` and `Annotations` fields
- `Annotation` struct captures message, path, line number, title, and severity level
- First 5 annotations per check are fetched via GraphQL
- Failed checks render a summary line and error box with file paths and messages

### Historical job averages

The `internal/github/history.go` module fetches historical job runtimes for ETA estimation using a two-phase approach:

- **Phase 1: Discovery** (`DiscoverWorkflows()`) - Resolves run IDs from current check run URLs to workflow IDs, respecting the incremental cache (`knownRunIDToWorkflowID`, `knownFetchedWorkflowIDs`)
- **Phase 2: Per-workflow fetch** (`FetchWorkflowHistory()`) - Fetches recent completed runs for each workflow ID in parallel, averaging job durations per job name
- The TUI dispatches these phases sequentially: `WorkflowsDiscoveredMsg` triggers parallel `FetchWorkflowHistory` calls, with each result delivered as a `JobAveragesPartialMsg`
- Uses incremental caching to avoid redundant API calls across polling cycles
- History fetch is gated by `minRateLimitForFetch` (100) and delayed by `historyFetchDelay` (10s) after first check appears
- Non-fatal: skips individual failures and returns whatever data was collected
- The legacy monolithic `FetchJobAverages()` remains available for snapshot mode

### Key timing calculations

The `internal/timing/calculator.go` module provides three core metrics:

1. **Queue latency** - Time from commit push to check start (`QueueLatency()`)
   - Calculated as: `check.StartedAt - headCommitTime`
   - Shows how long GitHub took to queue the job

2. **Runtime** - Elapsed time for in-progress checks (`Runtime()`)
   - Calculated as: `time.Now() - check.StartedAt`
   - Only for checks with status `in_progress`

3. **Final duration** - Total runtime for completed checks (`FinalDuration()`)
   - Calculated as: `check.CompletedAt - check.StartedAt`

### GitHub API client

The `internal/github/` package provides API interaction with both REST and GraphQL:

- `GetToken()` - Retrieves GitHub token using:
  1. `GITHUB_TOKEN` environment variable (first priority)
  2. `gh auth token` output (fallback)

- `NewClient()` - Creates authenticated REST API client for PR metadata

- `GetCurrentPR()` - Auto-detects PR number from current branch via `gh pr view`

- `GetCurrentPRWithRepo()` - Auto-detects PR number and owner/repo from current branch (correctly handles forked repos by deriving owner/repo from the PR URL)

- `GetPRWithRepo(prNumber)` - Fetches PR number and owner/repo for an explicit PR number (correctly handles forks)

- `ParseOwnerRepo()` - Extracts owner/repo from git remote origin (supports SSH and HTTPS formats)

- `ParsePRURL(prURL)` - Extracts owner, repo, and PR number from a GitHub PR URL (supports `https://github.com/owner/repo/pull/NNN` format)

- `FetchPRInfo()` - Retrieves PR metadata (title, SHA, timestamps) via REST API

- `FetchCheckRunsGraphQL()` - Fetches check runs with workflow names via GraphQL
  - Uses single GraphQL query for efficiency
  - Supports cursor-based pagination for PRs with more than 100 status contexts
  - Returns `CheckRunInfo` with workflow name, job name, status, timestamps, summary, and annotations

- `FetchCheckRuns()` - REST-based fallback for fetching check runs (used internally by snapshot mode)

- `FailureConclusion()` - Returns true if a conclusion indicates a failed check (failure, timed_out, or action_required)

- `ParseTimestamp()` - Parses GitHub API timestamps using `TimestampFormat` ("2006-01-02T15:04:05Z")

### Configuration system

User configuration lives in `~/.config/gh-observer/config.yaml`:

```yaml
refresh_interval: 5s  # How often to poll GitHub API
enable_links: true    # Whether to render hyperlinks in terminal (default: true)
colors:
  success: 10  # ANSI 256-color code for completed checks
  failure: 9   # ANSI 256-color code for failed checks
  running: 11  # ANSI 256-color code for in-progress checks
  queued: 8    # ANSI 256-color code for queued checks
```

The `internal/config/config.go` module uses Viper with defaults if config doesn't exist.

### Exit code behavior

The application returns meaningful exit codes:

- **0** - All checks passed successfully
- **1** - One or more checks failed (failure, timed_out, or action_required)
- **1** - Error during execution (authentication, network, etc.)

Exit code determination happens in `internal/tui/update.go`:

- `allChecksComplete()` checks if all checks have status `completed`
- `determineExitCode()` scans for failure conclusions and returns 1 if any found
- The TUI automatically quits when all checks complete

### Rate limit handling

The application tracks GitHub API rate limits:

- `ChecksUpdateMsg` includes `RateLimitRemaining` from API response
- When remaining < 10, the refresh interval triples (`m.refreshInterval * 3`)
- Default rate limit assumption is 5000 if not available in response

### Startup phase handling

The TUI has special handling for GitHub Actions startup delay (typically 30-90s):

- Displays helpful "Startup Phase" message while waiting for checks
- Shows elapsed time since PR creation
- Only polls for check runs after receiving the head SHA from PR info

## Building and running

### Build from source

```bash
git clone https://github.com/fini-net/gh-observer.git
cd gh-observer
just build
./gh-observer
```

### Install via go install

```bash
go install github.com/fini-net/gh-observer@latest
```

### Usage patterns

```bash
# Auto-detect PR from current branch
gh-observer

# Watch specific PR number
gh-observer 123

# Watch PR on external repository by URL
gh-observer https://github.com/owner/repo/pull/123

# Use in CI pipelines (exits with check status)
gh-observer && echo "All checks passed!"
```

## Dependencies

### Required tools

- `go` 1.26+ - Go programming language
- `gh` - GitHub CLI (for auth and PR detection)
- `git` - Version control

### Optional tools

- `just` - Command runner for development tasks

### Go dependencies

- `charm.land/bubbletea/v2` - TUI framework (Elm Architecture)
- `charm.land/lipgloss/v2` - Terminal styling and layout
- `charm.land/bubbles/v2` - Reusable TUI components (spinner)
- `github.com/google/go-github/v84` - GitHub REST API client (PR metadata and log fetching)
- `github.com/shurcooL/githubv4` - GitHub GraphQL API client (check runs)
- `github.com/spf13/cobra` - CLI framework for command-line argument parsing
- `github.com/spf13/viper` - Configuration management
- `golang.org/x/oauth2` - OAuth2 authentication for GitHub
- `golang.org/x/term` - Terminal detection for snapshot vs interactive mode
- `github.com/muesli/termenv` - Terminal color profile detection

## Important implementation notes

- PR detection uses `gh pr view` command and parses JSON output
- Owner/repo parsing supports both SSH (`git@github.com:owner/repo.git`) and HTTPS formats
- Check runs fetched via **GraphQL** for efficiency (single query gets workflow names)
- PR metadata fetched via **REST API** (simpler for basic PR info)
- GraphQL status/conclusion values normalized to lowercase for consistency
- All timestamps from GitHub API are parsed in RFC3339 format
- The TUI uses a spinner for visual feedback during polling
- Keyboard input is limited to 'q' and 'ctrl+c' for quitting
- Network errors during polling are non-fatal (stored in `m.err` but polling continues)
- The application polls every 5s by default, configurable via `refresh_interval`
- Terminal detection uses `term.IsTerminal(os.Stdout.Fd())` to switch between TUI and snapshot modes
- `--quick` / `-q` flag skips fetching historical average runtimes (faster startup, no ETA estimation)
- `.repo.toml` file configures repo metadata and feature flags (used by just recipes for Claude/Copilot reviews)

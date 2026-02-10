# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

gh-observer is a GitHub PR check watcher CLI tool that improves on `gh pr checks --watch` by showing runtime metrics, queue latency, and better handling of startup delays. Built as a Go application with a TUI (Terminal User Interface) using Bubbletea.

## Development Workflow

This repo uses `just` for all development tasks:

- `just build` - Build the `gh-observer` binary
- `just branch <name>` - Create a new feature branch (format: `$USER/YYYY-MM-DD-<name>`)
- `just pr` - Create PR, push changes, and watch checks
- `just merge` - Squash merge PR, delete branch, return to main, and pull latest
- `just again` - Push changes, update PR description, and watch GHAs

## Architecture

### High-level structure

gh-observer follows a clean architecture with distinct layers:

1. **Main entry point** (`main.go`) - Handles command-line arguments, configuration loading, and TUI initialization
2. **GitHub client layer** (`internal/github/`) - Abstracts GitHub API interactions
3. **TUI layer** (`internal/tui/`) - Implements Bubbletea model/view/update pattern
4. **Configuration** (`internal/config/`) - Loads user config from `~/.config/gh-observer/config.yaml`
5. **Timing utilities** (`internal/timing/`) - Calculates queue latency, runtime, and formats durations

### Bubbletea TUI architecture

The TUI follows the Elm Architecture pattern (Model-View-Update):

- **Model** (`internal/tui/model.go`) - Application state including PR metadata, check runs, rate limits, and UI state
- **Init** (`internal/tui/update.go`) - Initializes the model and kicks off PR info fetch
- **Update** (`internal/tui/update.go`) - Message handler that processes:
  - `TickMsg` - Periodic refresh (every 5s configurable)
  - `PRInfoMsg` - PR metadata (title, SHA, timestamps)
  - `ChecksUpdateMsg` - Check run status updates
  - `tea.KeyMsg` - Keyboard input (q to quit)
- **View** (`internal/tui/view.go`) - Renders the terminal UI
- **Messages** (`internal/tui/messages.go`) - Custom message types for async operations

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

**Display format** - Check names shown as "Workflow Name / Job Name":

- "CUE Validation / verify"
- "MarkdownLint / lint"
- "Claude Code Review / claude-review"
- "Checkov" (legacy checks without workflow show job name only)

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

- `ParseOwnerRepo()` - Extracts owner/repo from git remote origin (supports SSH and HTTPS formats)

- `FetchPRInfo()` - Retrieves PR metadata (title, SHA, timestamps) via REST API

- `FetchCheckRunsGraphQL()` - Fetches check runs with workflow names via GraphQL
  - Uses single GraphQL query for efficiency
  - Returns `CheckRunInfo` with workflow name, job name, status, and timestamps
  - Matches the approach used by `gh pr checks --watch`

### Configuration system

User configuration lives in `~/.config/gh-observer/config.yaml`:

```yaml
refresh_interval: 5s  # How often to poll GitHub API
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

# Use in CI pipelines (exits with check status)
gh-observer && echo "All checks passed!"
```

## Dependencies

### Required tools

- `go` 1.21+ - Go programming language
- `gh` - GitHub CLI (for auth and PR detection)
- `git` - Version control

### Optional tools

- `just` - Command runner for development tasks

### Go dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework (Elm Architecture)
- `github.com/charmbracelet/lipgloss` - Terminal styling and layout
- `github.com/charmbracelet/bubbles` - Reusable TUI components (spinner)
- `github.com/google/go-github/v58` - GitHub REST API client (PR metadata)
- `github.com/shurcooL/githubv4` - GitHub GraphQL API client (check runs)
- `github.com/spf13/viper` - Configuration management
- `golang.org/x/oauth2` - OAuth2 authentication for GitHub

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

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

gh-observer is a GitHub PR check watcher and Actions run monitor that improves on `gh pr checks --watch` by showing runtime metrics, queue latency, and better handling of startup delays. Built as a Go application with a TUI (Terminal User Interface) using Bubbletea. It supports watching both PR checks (by PR number or PR URL) and standalone Actions workflow runs (by run URL).

## Development Workflow

This repo uses `just` for all development tasks (recipes imported from `.just/`):

### DCO Compliance

Every commit **must** include a `Signed-off-by:` trailer to satisfy the Developer Certificate of Origin (DCO) policy. Use `git commit -s`. See [CONTRIBUTING.md](.github/CONTRIBUTING.md).

### Common commands

- `just build` - Build binary and install locally as gh extension
- `just branch <name>` - Create feature branch (`$USER/YYYY-MM-DD-<name>`)
- `just pr` - Create PR, push, and watch checks
- `just again` - Push changes, update PR description, watch GHAs (most common iterative workflow)
- `just merge` - Squash merge PR, delete branch, return to main
- `just release v1.0.0` - Create release tag; GHA builds cross-platform binaries automatically

### Testing

- `just test` or `go test ./...` - Run all unit tests
- `go test ./internal/timing/...` - Run a specific package's tests

The TUI and live GitHub API interactions are tested manually via `just build` against a real PR.

### Code quality

Pre-commit hooks via `.pre-commit-config.yaml`: `golangci-lint`, `shellcheck`, `gitleaks`, standard hooks. CI also runs `zizmor` (GHA security linting, config in `.github/zizmor.yml`).

## Architecture

### High-level structure

gh-observer follows a clean architecture with distinct layers:

1. **`main.go`** - Cobra CLI, config loading, mode selection. `parseArgs()` detects PR number, PR URL, or Actions run URL → sets `runMode` enum (`modePR` or `modeRun`)
2. **`internal/github/`** - GitHub API (REST + GraphQL). Covers checks, PRs, runs, repos, GraphQL queries, and history fetching
3. **`internal/tui/`** - Bubbletea TUI (Elm Architecture). PR mode: `model.go`/`update.go`/`view.go`. Run mode: `runmodel.go`/`runupdate.go`/`runview.go`. Shared: `display.go`, `styles.go`, `messages.go`, `constants.go`
4. **`internal/config/`** - Viper config from `~/.config/gh-observer/config.yaml`; see `.config.example.yaml`
5. **`internal/timing/`** - Queue latency, runtime, and duration calculations
6. **`internal/debug/`** - `slog`-based debug logging to `os.TempDir()/gh-observer-debug/` (enabled via `--debug`/`-d`)

### Two orthogonal execution modes

**Input type** (from `parseArgs()`):

- **PR mode** - Watches checks on a PR; accepts PR number, PR URL, or auto-detects from current branch
- **Run mode** - Watches jobs in a standalone Actions run URL. No queue latency column (no commit push event to reference)

**Output type** (from `term.IsTerminal(os.Stdout.Fd())`):

- **Interactive** (terminal) - Bubbletea TUI, live updates, auto-quits when all checks complete
- **Snapshot** (piped/CI) - Single-shot plain text output, exits with check status code

### TUI message flow

Key async message sequence for PR mode:

1. `PRInfoMsg` arrives → head SHA known → start polling checks
2. After `historyFetchDelay` (10s) and with rate limit > `minRateLimitForFetch` (100): dispatch `DiscoverWorkflows()`
3. `WorkflowsDiscoveredMsg` → dispatch parallel `FetchWorkflowHistory()` per workflow
4. `JobAveragesPartialMsg` per workflow → merge into ETA estimates
5. `allChecksComplete()` → `determineExitCode()` → quit

Run mode follows the same pattern with `RunInfoMsg` / `JobsUpdateMsg`.

### GraphQL architecture

`internal/github/graphql.go` fetches check runs via a single query:

```text
Repository → PullRequest → Commits → StatusCheckRollup → CheckRun
  → CheckSuite → WorkflowRun → Workflow → Name
```

Check names display as "Workflow Name / Job Name". Failed checks include inline error annotations (first 5 per check via GraphQL). Status/conclusion values normalized to lowercase. Supports cursor-based pagination for PRs with >100 status contexts.

### Historical job averages (ETA)

`internal/github/history.go` uses a two-phase incremental approach:

- **Phase 1: `DiscoverWorkflows()`** - Resolves run IDs from check URLs → workflow IDs; incremental cache (`knownRunIDToWorkflowID`, `knownFetchedWorkflowIDs`) avoids redundant calls
- **Phase 2: `FetchWorkflowHistory()`** - Parallel fetch of recent completed runs per workflow, averaging job durations per job name
- `--quick`/`-q` flag skips this entirely (faster startup, no ETA)
- Legacy monolithic `FetchJobAverages()` still available for snapshot mode

### Key timing calculations (`internal/timing/calculator.go`)

- **Queue latency**: `check.StartedAt - headCommitTime` (how long GitHub took to queue)
- **Runtime**: `time.Now() - check.StartedAt` (in_progress only)
- **Final duration**: `check.CompletedAt - check.StartedAt`

### Rate limit handling

`ChecksUpdateMsg` carries `RateLimitRemaining`. When remaining < `rateBackoffThreshold` (10), refresh interval triples. History fetch gated at `minRateLimitForFetch` (100).

### Jujutsu (jj) VCS compatibility

`IsJujutsu()` walks up from cwd for a `.jj/` directory. When detected, `SetGITDirForJJ()` runs `jj git root` and sets `GIT_DIR` on `gh pr view` commands so they can find the branch in non-colocated repos. Detection and root path are cached via `sync.Once`.

### Exit codes

- **0** - All checks passed
- **1** - Any check failed (`failure`, `timed_out`, or `action_required`), or execution error

### Release workflow

`just release v1.0.0` creates the GitHub release tag. Pushing a `v*` tag triggers `.github/workflows/release.yml`, which builds cross-platform binaries via `gh-extension-precompile`, signs with keyless cosign, and generates SLSA Level 3 provenance. Binaries are named `<os>-<arch>` (e.g., `darwin-arm64`) — **not** `<repo>_<version>_<os>-<arch>`. The signing and SLSA steps glob on this pattern and will fail silently if renamed.

## Important implementation notes

- PR detection: `gh pr view` + JSON parsing; supports both SSH (`git@github.com:owner/repo.git`) and HTTPS remote formats
- GraphQL for check runs (single query gets workflow names); REST for PR metadata
- Network errors during polling are non-fatal (stored in `m.err`, polling continues)
- Startup phase: TUI shows "Startup Phase" message while waiting for checks (typically 30-90s after PR creation)
- `.repo.toml` configures repo metadata for Claude/Copilot review just recipes
- `.opencode/` directory provides OpenCode AI editor integration

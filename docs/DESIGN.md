# Design Documentation

This document describes the architecture, actors, actions, and data flows
within gh-observer to satisfy the OpenSSF Best Practices requirement for
design documentation demonstrating all actions and actors within the system.

## System Overview

gh-observer is a CLI tool that watches GitHub PR check runs, displaying
runtime metrics, queue latency, and historical averages. It operates in two
modes: an interactive TUI (terminal user interface) when stdout is a terminal,
and a snapshot mode (plain text) when stdout is not a terminal (e.g., CI
pipelines).

## Actors

An actor is any entity that performs actions within or interacts with the
system.

| Actor | Type | Description |
| ----- | ---- | ----------- |
| User | Human | Invokes the CLI, provides PR number/URL or relies on auto-detection, presses `q`/`Ctrl+C` to quit the TUI |
| CLI Framework (Cobra) | Software | Parses command-line flags and positional arguments, validates input, dispatches to the run function |
| Config Loader (Viper) | Software | Reads `~/.config/gh-observer/config.yaml`, merges with defaults, provides configuration to the system |
| Terminal Detector | Software | Determines interactive vs snapshot mode by checking if stdout is a TTY |
| TUI Runtime (Bubbletea) | Software | Manages the Model-View-Update loop: renders the screen, processes keyboard input, dispatches async commands |
| GitHub REST API | External Service | Provides PR metadata, workflow run details, and job history via REST endpoints |
| GitHub GraphQL API | External Service | Provides check runs with workflow names, annotations, and pagination via a single GraphQL query |
| GitHub CLI (`gh`) | External Process | Provides authentication token and PR context detection when invoked as a subprocess |
| Debug Logger | Software | Optional structured logger (`slog`) that writes to `os.TempDir()/gh-observer-debug/` when `--debug` is set |
| Timing Calculator | Software | Computes queue latency, runtime, and final duration from GitHub timestamps |
| Display Formatter | Software | Aligns columns, renders hyperlinks, maps check status to icons and colors |
| History Fetcher | Software | Two-phase async component that discovers workflow IDs and fetches historical job duration averages |
| Rate Limiter | Software | Monitors GitHub API rate limit remaining, triples poll interval when approaching limits |

## Actions

Actions are the operations each actor performs within the system.

### User Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Invoke CLI | Run `gh-observer`, `gh-observer 123`, or `gh-observer https://github.com/owner/repo/pull/123` | Starts the application |
| Pass `--quick` / `-q` flag | CLI argument | Skips historical average runtime fetching |
| Pass `--debug` / `-d` flag | CLI argument | Enables structured debug logging to `os.TempDir()/gh-observer-debug/` |
| Press `q` | Keyboard input in TUI | Quits the application |
| Press `Ctrl+C` | Keyboard input in TUI | Quits the application |
| Provide PR number | CLI positional argument | Watches specific PR on the current repository |
| Provide PR URL | CLI positional argument | Watches specific PR on any accessible repository |
| No argument | CLI invocation without positional arg | Auto-detects PR from current git branch via `gh pr view` |
| Configure settings | Edit `~/.config/gh-observer/config.yaml` | Changes refresh interval, colors, and hyperlink behavior |

### CLI Framework Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Parse flags | Application start | Extracts `--quick` and `--debug` flags |
| Validate arguments | Application start | Ensures at most one positional argument (PR number or URL) |
| Dispatch to `run()` | After parsing | Passes args to the main run function |

### Config Loader Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Read config file | `config.Load()` call | Reads `~/.config/gh-observer/config.yaml` via Viper |
| Apply defaults | Missing or partial config | Uses default values: `refresh_interval=5s`, `enable_links=true`, ANSI color codes |
| Return config | After loading | Returns `Config` struct to caller |

### Terminal Detector Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Check stdout | `term.IsTerminal(os.Stdout.Fd())` | Determines whether to run in interactive (TUI) or snapshot mode |

### TUI Runtime Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Initialize model | `tui.NewModel()` | Creates Model with PR context, config, and UI state |
| Start program | `tea.NewProgram(model).Run()` | Enters the Bubbletea event loop |
| Dispatch init commands | `Init()` | Starts spinner, fetches PR info, starts poll timer |
| Handle TickMsg | Poll timer fires (every 5s default) | Dispatches async check run fetch and reschedules timer |
| Handle PRInfoMsg | PR info fetch completes | Stores PR metadata, triggers first check run fetch |
| Handle ChecksUpdateMsg | Check run fetch completes | Updates check runs, sorts, checks completion, dispatches history fetch if ready |
| Handle WorkflowsDiscoveredMsg | Workflow discovery completes | Merges run-to-workflow mappings, dispatches per-workflow history fetches |
| Handle JobAveragesPartialMsg | Per-workflow history fetch completes | Merges averages, marks workflow as fetched, checks if all fetches done |
| Handle ErrorMsg | Async operation fails | Stores error for display (non-fatal for polling errors) |
| Handle keyboard input | User presses key | Processes `q` or `Ctrl+C` as quit |
| Render screen | `View()` called after each Update | Draws header, check run table, startup phase messages, error details |
| Quit | All checks complete or user quits | Returns final model with exit code |

### GitHub REST API Actions

| Action | Endpoint | Purpose |
| ------ | --------- | ------- |
| Fetch PR metadata | `GET /repos/{owner}/{repo}/pulls/{pr_number}` | Get PR title, head SHA, created_at |
| Fetch commit timestamp | `GET /repos/{owner}/{repo}/commits/{sha}` | Get committer date for queue latency |
| Resolve workflow run | `GET /repos/{owner}/{repo}/actions/runs/{run_id}` | Map run ID to workflow ID |
| List workflow runs | `GET /repos/{owner}/{repo}/actions/workflows/{id}/runs` | Get recent completed runs for averages |
| List workflow jobs | `GET /repos/{owner}/{repo}/actions/runs/{id}/jobs` | Get job durations for averaging |

### GitHub GraphQL API Actions

| Action | Purpose |
| ------ | ------- |
| Fetch check runs (paginated) | Single query traverses Repository -> PullRequest -> Commits -> StatusCheckRollup -> Contexts (100 per page) |
| Resolve union types | Discriminates `CheckRun` vs `StatusContext` within the rollup |
| Fetch annotations | Retrieves up to 5 annotations per failed check run |
| Track rate limit | Reads `RateLimit.Remaining` from each response |

### GitHub CLI (`gh`) Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Provide auth token | `GITHUB_TOKEN` env var is unset | Executes `gh auth token` subprocess to retrieve OAuth token |
| Detect PR number | No positional argument given | Executes `gh pr view --json number,url` to auto-detect PR from current branch |

### Debug Logger Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Enable logging | `--debug` flag set | Creates `debug-<timestamp>.log` under the system temp directory in `gh-observer-debug/` (i.e., `filepath.Join(os.TempDir(), "gh-observer-debug")`), configures `slog` at Debug level |
| Write log entry | `debug.Log()` called throughout codebase | Writes structured key-value pairs via `slog.Debug()` |
| Noop | `--debug` flag not set | All `debug.Log()` calls are no-ops (guarded by `enabled` bool) |

### Timing Calculator Actions

| Action | Input | Output |
| ------ | ----- | ------ |
| Calculate queue latency | `check.StartedAt - headCommitTime` | Duration showing how long GitHub queued the job |
| Calculate runtime | `time.Now() - check.StartedAt` | Duration for in-progress checks |
| Calculate final duration | `check.CompletedAt - check.StartedAt` | Duration for completed checks |
| Format duration | `time.Duration` | Human-readable string (e.g., `3m 52s`, `15s`) |

### Display Formatter Actions

| Action | Purpose |
| ------ | ------- |
| Sort check runs | Primary by runtime ascending, secondary by status priority (in_progress > completed > queued), tertiary alphabetically |
| Format check name | Renders "WorkflowName / JobName" or "AppName / JobName" depending on available metadata |
| Build hyperlinks | Wraps check names in OSC 8 terminal hyperlinks when `enable_links` is true |
| Align columns | Calculates and pads column widths for tabular display |
| Map status to icons | `completed/success` -> checkmark, `completed/failure` -> cross, `in_progress` -> hourglass, `queued` -> clock |
| Map status to colors | Uses ANSI 256-color codes from user config or defaults |

### History Fetcher Actions

| Action | Phase | Purpose |
| ------ | ----- | ------- |
| Extract run IDs from check URLs | Phase 1 (Discovery) | Parses `DetailsURL` with regex `/actions/runs/(\d+)/job/` |
| Resolve run IDs to workflow IDs | Phase 1 (Discovery) | Calls REST API `GetWorkflowRunByID()` for each new run ID |
| Cache mappings | Phase 1 (Discovery) | Stores run-to-workflow mappings to avoid redundant API calls across polling cycles |
| Fetch recent completed runs per workflow | Phase 2 (Per-workflow) | Calls `ListWorkflowRunsByID()` with `status=completed`, up to 10 runs |
| Fetch jobs per run | Phase 2 (Per-workflow) | Calls `ListWorkflowJobs()` for each run |
| Average job durations | Phase 2 (Per-workflow) | Groups by job name, computes mean duration across runs |

### Rate Limiter Actions

| Action | Trigger | Effect |
| ------ | ------- | ------ |
| Track remaining quota | Each GraphQL response includes `RateLimit.Remaining` | Updates `rateLimitRemaining` in model |
| Back off polling | `rateLimitRemaining < 10` | Triples the refresh interval (e.g., 5s -> 15s) |
| Suppress history fetch | `rateLimitRemaining < 100` | Skips workflow discovery and history fetching |
| Assume default limit | Rate limit not available in response | Uses 5000 as default remaining |

## Data Flow

### Interactive Mode (TUI)

```text
User invokes CLI
  |
  v
[Cobra] parses flags & args
  |
  v
[Config Loader] reads ~/.config/gh-observer/config.yaml
  |
  v
[GitHub CLI] retrieves auth token (if GITHUB_TOKEN unset)
  |
  v
[PR Detection] resolves owner/repo/prNumber from args or current branch
  |
  v
[Terminal Detector] checks if stdout is a TTY
  |
  v (TTY = interactive)
[TUI Runtime] creates Model and enters event loop
  |
  +-- Init --> fetchPRInfo (REST) + spinner + tick timer
  |
  +-- PRInfoMsg --> store metadata, dispatch fetchCheckRuns (GraphQL)
  |
  +-- TickMsg (every 5s) --> dispatch fetchCheckRuns (GraphQL)
  |                       +-- reschedule tick (3x interval if rate-limited)
  |
  +-- ChecksUpdateMsg --> update checkRuns, sort, render
  |   +-- If first check seen: start historyFetchDelay timer
  |   +-- If delay elapsed & rate limit ok: dispatch discoverWorkflows (REST)
  |   +-- If all checks complete: set exit code, prepare to quit
  |
  +-- WorkflowsDiscoveredMsg --> merge mappings, dispatch fetchWorkflowHistory per workflow (REST)
  |
  +-- JobAveragesPartialMsg --> merge averages, check if all fetches done
  |
  +-- KeyMsg (q / Ctrl+C) --> quit
  |
  v
[Exit] os.Exit(exitCode)
```

### Snapshot Mode (Non-interactive)

```text
User invokes CLI (stdout is not a TTY)
  |
  v
[Cobra] -> [Config] -> [Auth] -> [PR Detection]
  |
  v (not a TTY)
[REST Client] fetches PR info (title, SHA, commit time)
  |
  v
[GraphQL Client] fetches check runs single query
  |
  v
[History Fetcher] fetches job averages (unless --quick)
  |
  v
[Display Formatter] calculates column widths, formats plain text
  |
  v
[Output] prints check status table to stdout
  |
  v
[Exit] os.Exit(0 if all pass, 1 if any fail)
```

### GraphQL Query Structure

```text
Repository(owner, name)
  +-- PullRequest(number)
       +-- Commits(last: 1)
            +-- Commit
                 +-- StatusCheckRollup
                      +-- Contexts(first: 100, after: cursor)
                           +-- ... on CheckRun
                           |    +-- Name
                           |    +-- Summary
                           |    +-- Status, Conclusion
                           |    +-- StartedAt, CompletedAt
                           |    +-- DetailsURL
                           |    +-- Annotations(first: 5)
                           |    +-- CheckSuite
                           |         +-- WorkflowRun.Workflow.Name
                           |         +-- App.Name
                           +-- ... on StatusContext
                                +-- Context, Description, State, TargetURL
  +-- RateLimit { Remaining }
```

Pagination: when `PageInfo.HasNextPage` is true, fetches next page with
`EndCursor` until all contexts are retrieved.

### Historical Averages Data Flow

```text
CheckRunInfo.DetailsURL
  |
  v
[Regex] extract run ID from /actions/runs/{run_id}/job/...
  |
  v
[Cache check] knownRunIDToWorkflowID hit?
  |              |
  | yes: skip    | no: REST GetWorkflowRunByID(runID) -> workflowID
  |              v
  |         [Cache] store runID -> workflowID mapping
  |
  v
For each new workflowID (not in fetchedWorkflowIDs):
  |
  v
REST ListWorkflowRunsByID(workflowID, status=completed, per_page=10)
  |
  v
For each run: REST ListWorkflowJobs(runID, filter=latest, per_page=100)
  |
  v
For each job with StartedAt + CompletedAt: duration = CompletedAt - StartedAt
  |
  v
Group by job name, average durations -> map[jobName]averageDuration
```

## Authentication

The system uses a two-source token resolution strategy:

1. **Environment variable** (`GITHUB_TOKEN`): First priority. Standard for
   CI/CD workflows and automated environments.
2. **GitHub CLI** (`gh auth token`): Fallback. Leverages the user's existing
   `gh` authentication (browser-based OAuth, device flow, or personal access
   tokens).

The token is used as an OAuth2 static token for both the GraphQL client and
the REST client. Failure to obtain a token from either source results in a
fatal error with the message:
`"authentication failed: set GITHUB_TOKEN or run 'gh auth login'"`

## Configuration

User configuration is loaded from `~/.config/gh-observer/config.yaml`.
Missing files or partial configs use sensible defaults.

```yaml
refresh_interval: 5s
enable_links: true
colors:
  success: 10
  failure: 9
  running: 11
  queued: 8
```

| Setting | Type | Default | Purpose |
| ------- | ---- | ------- | ------- |
| `refresh_interval` | Go duration | `5s` | Polling frequency for check run updates |
| `enable_links` | bool | `true` | Render OSC 8 terminal hyperlinks |
| `colors.success` | ANSI 256-color | `10` (green) | Color for passed checks |
| `colors.failure` | ANSI 256-color | `9` (red) | Color for failed checks |
| `colors.running` | ANSI 256-color | `11` (yellow) | Color for in-progress checks |
| `colors.queued` | ANSI 256-color | `8` (gray) | Color for queued checks |

## Error Handling

| Error Source | TUI Mode | Snapshot Mode |
| ------------ | -------- | ------------- |
| Authentication failure | Fatal (exit 1) | Fatal (exit 1) |
| PR detection failure | Fatal (exit 1) | Fatal (exit 1) |
| Config load failure | Fatal (exit 1) | Fatal (exit 1) |
| PR info fetch failure | Fatal (quit TUI, exit 1) | Fatal (exit 1) |
| Check run fetch failure | Non-fatal (stores error, continues polling) | Fatal (exit 1) |
| Workflow discovery failure | Non-fatal (skips averages) | Non-fatal (skips averages) |
| Individual workflow history failure | Non-fatal (graceful degradation) | Non-fatal (graceful degradation) |
| Network error during polling | Non-fatal (error displayed, next tick retries) | N/A (single fetch) |

## Exit Codes

| Condition | Exit Code |
| --------- | --------- |
| All checks passed | 0 |
| Any check has failure conclusion (`failure`, `timed_out`, `action_required`) | 1 |
| Application error (auth, config, PR detection) | 1 |
| No checks found (snapshot mode) | 0 |

## Rate Limiting

| Rate Limit Remaining | Behavior |
| -------------------- | -------- |
| >= 100 | Normal polling + history fetching allowed |
| 10-99 | Normal polling, history fetching suppressed |
| < 10 | Poll interval triples (e.g., 5s -> 15s) |
| Unavailable | Assume 5000 remaining |

## Startup Phase Handling

The TUI provides contextual messages during GitHub Actions' typical 30-90s
startup delay:

| Time Since Push | Message |
| --------------- | ------- |
| < 2 minutes | "Startup Phase" with "Waiting for Actions to start..." |
| 2-3 minutes | "Still waiting" with "Checks may be delayed" |
| > 3 minutes | "No checks found" with "workflows may not be configured" |

## Security Considerations

- **Token handling**: The GitHub token is held in memory only; never written to
  logs or persistent storage. Debug logs redact sensitive data.
- **No secrets in config**: The configuration file contains only display
  preferences (colors, intervals), not credentials.
- **No inbound network**: The application only makes outbound HTTPS requests to
  `api.github.com` and `github.com`. It listens on no ports.
- **No file writes** (except debug): The application does not modify any files
  on the user's system unless `--debug` is enabled, in which case it writes
  debug logs under the OS temporary directory (e.g., `os.TempDir()/gh-observer-debug/`).
- **Subprocess execution**: Only `gh` is executed as a subprocess, and only for
  authentication and PR detection. No user-supplied arguments are passed to
  shell execution.

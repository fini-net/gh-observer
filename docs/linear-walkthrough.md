# Linear Code Walkthrough

This document provides a comprehensive walkthrough of the gh-observer codebase, following the execution flow from entry point through all code paths. It's designed for contributors who need a deep technical understanding of how the application works.

The binary is named `gh-observer` (the installable name when installed via `go install`); the user-facing help text and the `gh` extension invocation prefer `gh observer` without the dash. This doc uses `gh-observer` when referring to the binary and `gh observer` when quoting help text.

gh-observer runs in one of three input modes, selected by parsing the argument:

- **PR mode** (`modePR`) — watches checks on a pull request (PR number, PR URL, or auto-detected from the current branch)
- **Run mode** (`modeRun`) — watches jobs in a standalone Actions workflow run (Actions run URL)
- **Repo mode** (`modeRepo`) — persistently watches all active PR checks plus standalone branch runs on a repo (`--repo` flag; interactive only)

Every mode is also host-aware: PR/run URLs, `--repo host/owner/repo`, git remotes, and `GH_HOST` all resolve to a GitHub host (github.com or any GitHub Enterprise host), and every API client, token lookup, and URL parse threads that host through. Host-less arguments (a bare PR number or `owner/repo` slug) default to `GH_HOST` or github.com, mirroring the gh CLI.

PR and Run modes each support two output types: interactive TUI (terminal) and snapshot (non-terminal). Repo mode is always interactive. All three interactive modes render a pinned header and footer with a vim-style scrollable body between them, and gate exit on an optional Copilot code-review tracker (PR mode only).

## Table of Contents

1. [Application Entry Point](#1-application-entry-point)
2. [GitHub Authentication & Setup](#2-github-authentication--setup)
3. [Execution Path A: Snapshot Mode](#3-execution-path-a-snapshot-mode)
4. [Execution Path B: Interactive TUI Mode](#4-execution-path-b-interactive-tui-mode)
5. [TUI Message Processing Loop](#5-tui-message-processing-loop)
6. [GitHub API Layer Deep Dive](#6-github-api-layer-deep-dive)
7. [Timing Calculations](#7-timing-calculations)
8. [TUI Rendering System](#8-tui-rendering-system)
9. [Error Handling & Edge Cases](#9-error-handling--edge-cases)
10. [Data Flow Diagrams](#10-data-flow-diagrams)
11. [Exit Behavior](#11-exit-behavior)
12. [Repo Mode](#12-repo-mode)

---

## 1. Application Entry Point

### Command Registration (`main.go:26-90`)

The application uses [Cobra](https://github.com/spf13/cobra) for CLI argument parsing. The root command is registered with:

- **Usage**: `gh-observer [PR_NUMBER | PR_URL | ACTIONS_RUN_URL]`
- **Arguments**: Maximum of 1 positional argument (optional PR number, full PR URL, or Actions run URL)
- **Flags**:
  - `--quick` / `-q`: Skip fetching historical average runtimes
  - `--debug` / `-d`: Enable structured debug logging to `os.TempDir()/gh-observer-debug/`
  - `--repo` `[host/owner/repo|owner/repo|URL]`: Persistently watch all active workflows on a repo. Bare `--repo` (no value) auto-detects from the current git remote via pflag's `NoOptDefVal` sentinel. Incompatible with `--quick` or positional arguments and requires an interactive terminal.
- **Execution**: Calls `run(cmd, args)` and exits with the returned exit code

```go
var quickFlag bool
var debugFlag bool
var repoFlag string

// repoFlagAutoSentinel is the NoOptDefVal for --repo: when the user passes
// --repo with no value, pflag fills repoFlag with this sentinel so we can
// distinguish "no value given (auto-detect)" from "value given explicitly".
//
// The required properties are (a) not parseable as "owner/repo" or a
// GitHub URL, and (b) not something a user would type on purpose. A single
// underscore "_" satisfies both: ParseRepoArg in internal/github/repo.go
// explicitly rejects all-underscore segments (see isAllUnderscoreSegment),
// so `gh-observer --repo _` errors cleanly as an invalid argument rather
// than being mistaken for auto-detect or treated as a literal owner/repo.
const repoFlagAutoSentinel = "_"

func init() {
    rootCmd.Flags().BoolVarP(&quickFlag, "quick", "q", false, "Skip fetching historical average runtimes")
    rootCmd.Flags().BoolVarP(&debugFlag, "debug", "d", false, "Log suppressed errors and internal state to a file")
    rootCmd.Flags().StringVar(&repoFlag, "repo", "", "Watch all active workflows on a repo persistently (owner/repo, host/owner/repo, or URL; bare --repo auto-detects from current git remote)")
    rootCmd.Flags().Lookup("repo").NoOptDefVal = repoFlagAutoSentinel
}

var rootCmd = &cobra.Command{
    Use:   "gh-observer [PR_NUMBER | PR_URL | ACTIONS_RUN_URL]",
    Short: "Watch GitHub PR checks or Actions runs with runtime metrics",
    Long: `gh observer (invoked as gh-observer when installed via go install) is a
GitHub PR check watcher CLI tool that improves on 'gh pr checks --watch' by
showing runtime metrics, queue latency, and better handling of startup delays.
...
GitHub Enterprise URLs work with any hostname (issue #479). Authenticate first
with 'gh auth login --hostname <host>' or set GH_ENTERPRISE_TOKEN:
  gh observer https://github.example.com/owner/repo/pull/123
  gh observer https://github.example.com/owner/repo/actions/runs/123456789

Use --repo to persistently watch all active workflows on a repository:
  gh observer --repo              # auto-detect from current git remote
  gh observer --repo owner/repo
  gh observer --repo host/owner/repo
  gh observer --repo https://github.com/owner/repo
  gh observer --repo https://github.example.com/owner/repo

Hosts without an explicit URL or remote are resolved from GH_HOST (like the
gh CLI), defaulting to github.com.`,
    Args: cobra.MaximumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        exitCode := run(cmd, args)
        os.Exit(exitCode)
    },
}

// runMode determines which mode the application is running in.
type runMode int

const (
    modePR   runMode = iota // Watch a PR's checks
    modeRun                 // Watch an Actions workflow run
    modeRepo                // Watch all active workflows on a repo persistently
)

// runArgs holds the parsed arguments for either mode.
type runArgs struct {
    mode     runMode
    host     string
    owner    string
    repo     string
    prNumber int
    runID    int64
}
```

The `--repo` flag uses `NoOptDefVal = "_"` so `--repo` with no value (bare flag) is distinguishable from `--repo=VALUE`. The sentinel `_` is rejected by `ParseRepoArg` (which refuses all-underscore segments), so `gh-observer --repo _` errors cleanly instead of being mistaken for auto-detect or a literal owner/repo.

**Design Decision**: The exit code is captured and passed to `os.Exit()` explicitly. This allows the TUI to clean up properly before exiting.

### URL / Argument Support (`main.go:229-264` via `parseArgs()`)

`parseArgs()` returns a `runArgs` value whose `mode` and `host` fields select the execution path. It tries, in order:

```go
func parseArgs(args []string) (runArgs, error) {
    if len(args) == 0 {
        // Auto-detect PR from current branch (correctly handles forks; jj-aware)
        prNumber, host, owner, repo, err := ghclient.GetCurrentPRWithRepo()
        if err != nil {
            if ghclient.IsJujutsu() {
                return runArgs{}, fmt.Errorf("failed to detect PR in jj (Jujutsu) repo: %v\n\nHint: ...", err)
            }
            return runArgs{}, fmt.Errorf("failed to detect PR: %v\n...", err)
        }
        return runArgs{mode: modePR, host: host, owner: owner, repo: repo, prNumber: prNumber}, nil
    }

    arg := args[0]

    // Try PR URL first
    if host, owner, repo, prNumber, err := ghclient.ParsePRURL(arg); err == nil {
        return runArgs{mode: modePR, host: host, owner: owner, repo: repo, prNumber: prNumber}, nil
    }

    // Try Actions run URL
    if host, owner, repo, runID, err := ghclient.ParseActionsRunURL(arg); err == nil {
        return runArgs{mode: modeRun, host: host, owner: owner, repo: repo, runID: runID}, nil
    }

    // Try numeric PR number
    if n, convErr := strconv.Atoi(arg); convErr == nil {
        prNumber, host, owner, repo, err := ghclient.GetPRWithRepo(n)
        // ...
        return runArgs{mode: modePR, host: host, owner: owner, repo: repo, prNumber: prNumber}, nil
    }

    return runArgs{}, fmt.Errorf("invalid PR number, PR URL, or Actions run URL: %s", arg)
}
```

**Why try URLs first?** A PR URL or Actions run URL unambiguously identifies the target. Numeric values are treated as PR numbers only after URL parsing fails.

**Host extraction**: Both URL regexes capture the hostname (lowercased) and accept any host — GitHub Enterprise URLs included. Unknown hosts fail later at the auth/API layer rather than being rejected here, so a typo'd enterprise hostname produces an authentication error instead of "invalid URL". A host-less argument (bare PR number, or the auto-detected path) gets its host from `ParsePRViewWithRepo` (forks — below) or `DefaultHost()` (`GH_HOST` or github.com).

**Fork Handling**: `GetPRWithRepo()` and `GetCurrentPRWithRepo()` use `gh pr view --json number,url` and derive host/owner/repo from the PR URL — the local git remote may point at a fork (or an enterprise checkout), but the PR lives upstream at the host/owner/repo the URL names.

### Main Run Function (`main.go:111-209`)

The `run()` function orchestrates initialization and mode selection:

#### Step 1: Debug Logging Setup (`main.go:114-121`)

```go
if debugFlag {
    if err := debug.Enable(); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to enable debug logging: %v\n", err)
        return 1
    }
    defer debug.Close()
    fmt.Fprintf(os.Stderr, "Debug log: %s\n", debug.LogPath())
}
```

When `--debug` is enabled, structured debug logging via `slog` writes to `os.TempDir()/gh-observer-debug/`. Debug statements throughout the codebase log key events like check updates, rate limit backoff, endpoint derivation, Copilot gate arming, and completion trust decisions.

#### Step 2: Repo-mode validation (`main.go:123-148`)

```go
repoMode := cmd.Flags().Changed("repo")
bareRepoFlag := repoMode && repoFlag == repoFlagAutoSentinel

// Allow `gh-observer --repo owner/repo` by consuming a trailing positional.
if bareRepoFlag && len(args) == 1 {
    repoFlag = args[0]
    args = nil
}
if repoMode && len(args) > 0 {
    fmt.Fprintf(os.Stderr, "Error: --repo flag cannot be used with positional arguments\n")
    return 1
}
if repoMode && quickFlag {
    fmt.Fprintf(os.Stderr, "Error: --repo flag cannot be used with --quick\n")
    return 1
}
if repoMode && !term.IsTerminal(int(os.Stdout.Fd())) {
    fmt.Fprintf(os.Stderr, "Error: --repo flag requires an interactive terminal\n")
    return 1
}
```

Repo mode is the only mode that does **not** flow through `parseArgs()`: because `--repo` is incompatible with positionals, it's validated and dispatched before argument parsing.

#### Step 3: Configuration Loading (`main.go:150-155`)

```go
cfg, err := config.Load()
```

Calls `internal/config/config.go` which:

1. Creates a new Viper instance
2. Sets defaults:
   - `refresh_interval: 5s` (PR/run modes)
   - `repo_refresh_interval: 30s` (repo mode)
   - `fade_success: 15m` (repo mode: how long a passing completed check stays on screen)
   - `fade_failure: 30m` (repo mode: how long a failed completed check stays on screen)
   - `colors.success: 10` (green)
   - `colors.failure: 9` (red)
   - `colors.running: 11` (yellow)
   - `colors.queued: 8` (gray)
   - `enable_links: true`
   - `presumed_averages.DCO: 1s` (see [Presumed Averages](#historical-job-averages-internalgithubhistorygo))
   - `wait_for_copilot: true` (see [Copilot Code Review Tracking](#copilot-code-review-tracking-issues-409-442-486))
   - `copilot_max_wait: 180s`
   - `copilot_poll_interval: 10s`
   - `copilot_initial_delay: 15s`
3. Reads config from `~/.config/gh-observer/config.yaml` (if exists)
4. Falls back to defaults if config file missing
5. Unmarshals into `Config` struct

#### Step 4: Style Creation (`main.go:157-163`)

```go
styles := tui.NewStyles(
    cfg.Colors.Success,
    cfg.Colors.Failure,
    cfg.Colors.Running,
    cfg.Colors.Queued,
)
```

Creates Lipgloss styles for rendering colored output. See `internal/tui/styles.go` for implementation.

#### Step 5: Repo-mode dispatch (`main.go:165-182`)

If `--repo` was given, `resolveRepoArg(repoFlag)` resolves host/owner/repo (auto-detect via `ghclient.GetCurrentRepo()` reading `git remote get-url origin`, or parse via `ghclient.ParseRepoArg` — `owner/repo`, `host/owner/repo`, or URL). An empty host falls back to `ghclient.DefaultHost()`. A token check runs **before** the TUI starts so a host without credentials (e.g. a typo'd enterprise hostname) fails fast instead of mid-poll. Repo mode never reaches `parseArgs()`.

#### Step 6: Argument Parsing (`main.go:184-189`)

```go
parsed, err := parseArgs(args)
if err != nil {
    fmt.Fprintf(os.Stderr, "%v\n", err)
    return 1
}
```

Returns a `runArgs` value with `mode` set to `modePR` or `modeRun`, plus the resolved `host`.

#### Step 7: Authentication (`main.go:191-198`)

```go
token, err := ghclient.GetTokenForHost(parsed.host)
```

Token acquisition strategy (`GetTokenForHost`):

1. **github.com (or `GH_HOST`) host**: `GH_TOKEN`, then `GITHUB_TOKEN`, then `gh auth token`
2. **Any other (enterprise) host**: `GH_ENTERPRISE_TOKEN`, then `GITHUB_ENTERPRISE_TOKEN`, then `gh auth token --hostname <host>`
3. **Error**: Return message if all fail

This doubles as an early capability check: a host we can't authenticate against fails before the TUI starts.

#### Step 8: Mode Selection (`main.go:200-208`)

```go
switch parsed.mode {
case modePR:
    return runPRMode(ctx, token, parsed, cfg, styles)
case modeRun:
    return runActionsMode(ctx, token, parsed, cfg, styles)
}
```

`runPRMode` and `runActionsMode` each check `term.IsTerminal(os.Stdout.Fd())` internally:

- **Not a terminal** (piped, redirected, or CI): Runs snapshot mode (`runSnapshot` / `runRunSnapshot`)
- **Is a terminal**: Runs interactive TUI mode (`tui.NewModel` / `tui.NewRunModel`)

---

## 2. GitHub Authentication & Setup

### Host Resolution & Default Host (`internal/github/host.go:31-47`)

`DefaultHost()` mirrors the gh CLI: when `GH_HOST` is set to a valid hostname (letters, digits, dots, hyphens; no ports), it wins; otherwise the default is `DefaultGitHubHost` = `github.com`. Every host-bearing parser lowercases the host it captures (URLs, remotes, `host/owner/repo` slugs), and `GH_HOST` is validated the same way — invalid values are ignored with a debug log rather than crashing.

### API Endpoint Derivation (`internal/github/host.go:112-151`)

`APIURLsForHost(host)` derives the REST and GraphQL base URLs, mirroring go-gh — the gh CLI's own endpoint resolution — so gh-observer reaches the same endpoints the gh CLI does (issues #485, #532):

- **github.com**: empty strings — callers keep the library defaults (`api.github.com` and githubv4's default endpoint). A stray override never redirects public-host traffic.
- **`*.ghe.com` tenants** (GitHub Enterprise Cloud): the `api.<host>` subdomain convention — REST at `https://api.<host>/`, GraphQL at `https://api.<host>/graphql`
- **Every other enterprise host** (GitHub Enterprise Server, custom domains): the path-based endpoints GHES documents — REST at `https://<host>/api/v3/`, GraphQL at `https://<host>/api/graphql`. Many GHES installs also serve the subdomain form via subdomain isolation, but only the path form is reachable without it.

Two environment variables override the derivation for non-github.com hosts whose API is not reachable through it:

- `GH_OBSERVER_API_URL` — overrides both: REST at `<url>/` (go-github appends `api/v3/` unless the base already ends in `/api/v3`), GraphQL at `<url>/graphql`
- `GH_OBSERVER_GRAPHQL_URL` — overrides just GraphQL and wins over what the API override derives, so the pair can express split layouts (e.g. GHES behind a proxy)

Override values must be absolute `https` URLs (or plain `http` on a loopback host — the bearer token rides every request to the override target), are trimmed of trailing slashes, and invalid values are ignored with a debug log. The resolved pair is debug-logged — the first thing to check with `--debug` when a GHES host fails to connect.

### REST API Client Creation (`internal/github/host.go:267-282`)

```go
func NewClientFromToken(token, host string) (*github.Client, error) {
    restURL, _ := APIURLsForHost(host)
    if restURL == "" {
        return github.NewClient(github.WithAuthToken(token)), nil
    }
    baseURL, err := url.Parse(restURL)
    if err != nil {
        return nil, fmt.Errorf("invalid enterprise API URL for host %s: %w", host, err)
    }
    return github.NewClient(
        github.WithAuthToken(token),
        github.WithEnterpriseURLs(baseURL.String(), baseURL.String()),
    ), nil
}
```

Uses `google/go-github` (v93) with token authentication. The token itself is resolved by `GetTokenForHost(host)`: env vars first (`GH_TOKEN`/`GITHUB_TOKEN`, or the enterprise equivalents for non-github.com hosts), then `gh auth token` — with `--hostname=<host>` passed as a single `=`-joined token for enterprise hosts so it can never be misparsed as a flag (the gh CLI accepts `-h`/`--hostname` only; issue #479).

### GraphQL Client Creation (`internal/github/host.go:288-300`)

Check run fetching uses GraphQL for efficiency:

```go
func newGraphQLClient(host string, httpClient *http.Client) *githubv4.Client {
    _, graphqlURL := APIURLsForHost(host)
    if graphqlURL == "" {
        return githubv4.NewClient(httpClient)
    }
    return githubv4.NewEnterpriseClient(graphqlURL, httpClient)
}

func newAuthenticatedHTTPClient(ctx context.Context, token string) *http.Client {
    src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    return oauth2.NewClient(ctx, src)
}
```

**Design Decision: Why use both REST and GraphQL?**

- **REST for simple metadata**: PR info (title, SHA), workflow runs, and job lists are single-call lookups where REST is straightforward
- **GraphQL for check runs**: One query fetches workflow name + job name + status + timestamps + annotations + rate limit. Equivalent REST calls would require multiple API calls per check
- **GraphQL for push times**: The REST API only exposes commit author/committer timestamps; `pushedDate` — the anchor for all "since push" displays — requires GraphQL (issue #349)

### Rate Limit Normalization (`internal/github/client.go:26-47`)

GitHub Enterprise Server instances with rate limiting disabled (the default) return `rateLimit: null` in GraphQL (a zero-value struct) and omit `X-RateLimit-*` headers in REST. Reading that as "0 remaining" pinned the app in permanent rate-limit backoff while every request succeeded — issue #442's "192h elapsed" Copilot spin was exactly this. Two helpers fix it:

- `UnknownRateLimit = 5000` — mirrors github.com's default hourly quota so thresholds treat "unknown" the same as "plenty available"
- `normalizeRateLimit(limit, remaining)` — a real quota always carries a positive limit, so `limit <= 0` means the data is absent → unknown. A positive limit with `remaining 0` is preserved: backoff must engage for the requests that follow

Every fetch path funnels its rate-limit observation through this normalization and takes the minimum across pages/sources.

---

## 3. Execution Path A: Snapshot Mode

Snapshot mode runs when stdout is not a terminal (e.g., scripts, CI, redirected output). There are two snapshot variants — one per input mode — selected by `runPRMode` / `runActionsMode`:

### PR snapshot (`runSnapshot`, `main.go:378-474`)

#### Step 1: Fetch PR Metadata

```go
client, err := ghclient.NewClientFromToken(token, host)
prInfo, err := ghclient.FetchPRInfo(ctx, client, owner, repo, prNumber)
```

Uses REST API to get PR title and head SHA. The timing anchor is sourced separately from the GraphQL check-runs query (`FetchCheckRunsGraphQL`), which fetches the head commit's `pushedDate` and the PR's `createdAt` in the same round-trip as the StatusCheckRollup and combines them into the cycle-start anchor, `max(push, creation)` (issues #349 and #518).

#### Step 2: Fetch Check Runs

```go
checkRuns, cycleStart, _, err := ghclient.FetchCheckRunsGraphQL(ctx, token, host, owner, repo, prNumber)
```

Returns `[]CheckRunInfo` with workflow names, status, timestamps, and annotations, plus the cycle-start anchor: the later of the head commit's push time and the PR's creation time (issue #518), so a PR opened from an already-pushed branch doesn't read misleadingly old.

#### Step 3: Handle Empty Checks

```go
if len(checkRuns) == 0 {
    if !cycleStart.IsZero() {
        sinceStart := time.Since(cycleStart)
        fmt.Printf("No checks found (started %s ago)\n", timing.FormatDuration(sinceStart))
    } else {
        fmt.Println("No checks found")
    }
    fmt.Println("Checks may still be starting up or not configured for this PR")
    return 0
}
```

#### Step 4: Fetch Historical Averages (unless `--quick`) and Apply Presumed Averages

```go
var jobAverages map[string]time.Duration
if !quick {
    avgs, _, _, err := ghclient.FetchJobAverages(ctx, client, owner, repo, checkRuns, nil, nil)
    if err == nil {
        jobAverages = avgs
    }
}
if jobAverages == nil {
    jobAverages = make(map[string]time.Duration)
}
ghclient.ApplyPresumedAverages(jobAverages, checkRuns, presumedAverages, host)
```

`FetchJobAverages` (see [Historical Job Averages](#historical-job-averages-internalgithubhistorygo)) fetches recent completed workflow runs and computes a recency-weighted average per job name. `ApplyPresumedAverages` then fills any remaining external-app checks (e.g. DCO) with configured presumed durations so the HistAvg column isn't blank for checks that can never have Actions history.

#### Step 5: Calculate Column Widths and Render

```go
widths := tui.CalculateColumnWidths(checkRuns, cycleStart, jobAverages)
headerQueue, headerName, headerDuration, headerAvg := tui.FormatHeaderColumns(widths)
fmt.Printf("%s   %s  %s  %s\n\n", headerQueue, headerName, headerDuration, headerAvg)
```

**Output Format**:

```text
Start     Workflow/Job        ThisRun  HistAvg

42s    ✓  Build / test          2m 15s    2m 0s
1m 5s  ◐  Lint / check            1m 3s    45s
```

#### Step 6: Copilot Review Snapshot (issue #409, #442)

When `wait_for_copilot` is enabled (the default), the snapshot ends with a Copilot review line. The host capability probe runs first — an incapable host (e.g. GitHub Enterprise Server without Copilot code review) prints a single explanatory line instead of querying for reviews that cannot exist there:

```go
if waitForCopilot {
    if !ghclient.CopilotReviewerExistsOnHost(ctx, token, host) {
        fmt.Println("Copilot: not available on this host")
    } else {
        review, _, copilotErr := ghclient.FetchCopilotReview(ctx, token, host, owner, repo, prNumber, prInfo.HeadSHA)
        // Prints one of:
        //   Copilot: unavailable (<err>)
        //   Copilot: not requested
        //   Copilot: stale (review targets old commit)
        //   Copilot: in progress
        //   Copilot: <state>          (approved / changes_requested / ...)
        if ghclient.CopilotReviewFails(review.State) {
            exitCode = 1
        }
    }
}
```

#### Step 7: Exit Code Determination

```go
if check.Status == "completed" {
    if ghclient.FailureConclusion(check.Conclusion) {
        exitCode = 1
    }
}
```

Plus exit code 1 when the Copilot review requested changes (above).

### Actions run snapshot (`runRunSnapshot`, `main.go:477-550`)

Mirrors the PR snapshot but for a standalone workflow run:

```go
runInfo, _, err := ghclient.FetchRunInfo(ctx, client, token, host, owner, repo, runID)
jobs, _, err := ghclient.FetchRunJobs(ctx, client, owner, repo, runID)

// Header: display title, then "Pushed Xs ago" (or "Created Xs ago" when
// commit info is unavailable — FetchRunInfo sources the push time from a
// GraphQL pushedDate lookup with the REST head_commit.timestamp as fallback)
fmt.Printf("%s/%s: %s\n", owner, repo, runInfo.DisplayTitle)

// Optional history fetch (unless --quick) via WorkflowJobInfoToCheckRuns(jobs)
// to reuse FetchJobAverages, then ApplyPresumedAverages,
// then tui.CalculateRunColumnWidths / tui.FormatRunHeaderColumns for layout.

for _, job := range jobs {
    // icon, nameCol, durationText, avgText
    if job.Status == "completed" && ghclient.FailureJobConclusion(job.Conclusion) {
        exitCode = 1
    }
}
```

Run snapshot mode **omits the queue-latency (Start) column** because a standalone run isn't tied to a commit-push event the way a PR check is. Columns are `Workflow/Job | ThisRun | HistAvg` via `tui.CalculateRunColumnWidths` and `tui.FormatRunHeaderColumns`.

**Failure detection**: Uses `FailureJobConclusion()` (run-mode equivalent of `FailureConclusion()`) to match the conclusion vocabulary returned by the Actions jobs API.

---

## 4. Execution Path B: Interactive TUI Mode

TUI mode runs when stdout is a terminal, providing real-time updates. PR mode and Run mode each have their own model files but share display helpers, constants, the scroll subsystem, and the history-fetch pipeline. Repo mode has its own model and is covered in [Section 12](#12-repo-mode).

The TUI stack is bubbletea v2 (`charm.land/bubbletea/v2`) with bubbles v2 and lipgloss v2 — the model/update/view loop is the same Elm Architecture as v1, but `View()` now returns a `tea.View` struct (content plus terminal-mode flags) rather than a plain string.

### Model Creation

**PR mode** (`main.go:276`):

```go
model := tui.NewModel(ctx, token, host, owner, repo, prNumber, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag, cfg.PresumedAveragesDurations(), cfg.WaitForCopilot, cfg.CopilotMaxWait, cfg.CopilotPollInterval, cfg.CopilotInitialDelay)
```

**Run mode** (`main.go:304`):

```go
model := tui.NewRunModel(ctx, token, host, owner, repo, runID, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag, cfg.PresumedAveragesDurations())
```

The `type Model struct` definition (`internal/tui/model.go:12-140`) holds all PR-mode TUI state:

```go
type Model struct {
    // Context and GitHub data
    ctx      context.Context
    token    string
    host     string
    owner    string
    repo     string
    prNumber int

    // PR metadata (populated later)
    prTitle        string
    headSHA        string
    cycleStartTime time.Time

    // Check run data (updated every poll)
    checkRuns         []ghclient.CheckRunInfo
    rateLimitRemaining int
    fetchReceived     bool // true after the first response; gates the
                           // rate-limit indicator and backoff so the zero
                           // value (0) never misleads

    // UI state
    spinner         spinner.Model
    startTime       time.Time
    lastUpdate      time.Time
    refreshInterval time.Duration
    styles          Styles

    // Scrolling for check tables taller than the terminal (issue #554)
    scrollState

    // Exit tracking
    exitCode int
    quitting bool

    // Error state
    err error

    // Feature flags
    enableLinks bool

    // Historical job averages (incrementally updated)
    jobAverages             map[string]time.Duration
    workflowAverages        map[int64]map[string]time.Duration // per-workflow cache
    advSecMatchWorkflow     map[string]int64                  // AdvSec check name → workflow ID
    runIDToWorkflowID       map[int64]int64
    fetchedWorkflowIDs      map[int64]bool
    pendingWorkflowFetch    map[int64]bool
    dispatchedWorkflowFetch map[int64]bool
    avgFetchPending         bool
    avgFetchStartTime       time.Time
    avgFetchLastDuration    time.Duration
    avgFetchErr             error
    noAvg                   bool
    firstCheckSeenAt        time.Time

    // Set when all checks complete; used to defer quit until history fetches drain
    checksComplete bool

    // Premature exit prevention (issue #236)
    expectedCheckCount int
    peakCheckCount     int

    // Tracks which check runs we've already triggered discovery for,
    // so we can re-trigger when new jobs appear.
    seenCheckKeys         map[string]bool
    historyFetchCompleted bool

    // Presumed historical averages for external GitHub App checks (e.g. DCO)
    presumedAverages map[string]time.Duration

    // Copilot code review detection (issue #409). PR mode only — run mode
    // has no reviews object to query.
    waitForCopilot        bool
    copilotMaxWait        time.Duration
    copilotPollInterval   time.Duration
    copilotInitialDelay   time.Duration
    copilotState          string
    copilotStale          bool
    copilotPending        bool
    copilotWaitStartTime  time.Time
    copilotPollStartTime  time.Time
    copilotLastPoll       time.Time
    copilotNotReqStreak   int
    copilotReviewComplete bool
    copilotTimedOut       bool
    copilotEvidenceSeen   bool
    copilotHostChecked    bool
    copilotHostCapable    bool
}
```

**Premature Exit Prevention**: `expectedCheckCount` tracks how many distinct job names the history fetch has discovered (set from `len(m.jobAverages)` each time a partial result arrives). `peakCheckCount` tracks the maximum number of checks seen in any single poll. These fields power the `canTrustCompletion()` function that prevents exiting when fast checks (like DCO) complete before slower checks have even appeared in the API response.

**AdvSec Caching**: `workflowAverages` caches each workflow's fetched averages and `advSecMatchWorkflow` maps Advanced Security check names (CodeQL, Checkov) to matched Actions workflows so their history can be aliased under the check name.

The run-mode equivalent `RunModel` (`internal/tui/runmodel.go:13-84`) holds the equivalent state for a workflow run: `jobs []ghclient.WorkflowJobInfo`, `runInfo ghclient.RunInfo`, `runInfoLoaded bool`, the same incremental-history-fetch maps, plus a `seenJobKeys` dedup map and `historyFetchCompleted` flag to drive a single discovery cycle per unique job set. It pre-builds a `*github.Client` in `NewRunModel` so per-poll REST calls don't re-authenticate, and it has no Copilot fields — standalone runs have no reviews object to query.

### Program Initialization (`main.go:279-291`)

```go
p := tea.NewProgram(model)
finalModel, err := p.Run()
if err != nil { /* print, return 1 */ }

printFinalFrame(finalModel)
if m, ok := finalModel.(tui.Model); ok {
    return m.ExitCode()
}
```

Creates a Bubbletea program and enters the event loop. After `p.Run()` returns, `printFinalFrame` re-renders the final state to stdout (see below), then `main()` asserts on the concrete model type (`tui.Model` or `tui.RunModel`) and calls `m.ExitCode()`. For repo mode, the assertion uses a small `exitCoder` interface so it works whether `Update` returned a value or pointer `RepoModel` (the per-message handlers use pointer receivers).

### Alt-Screen Mode & the Final-Frame Reprint (`display.go:146-150`, `main.go:369-375`)

The TUI runs in the terminal's alternate screen buffer so stale frames from a crashed run can't corrupt the visible screen (issue #451). Every `View()` return path must set `AltScreen` on the returned `tea.View`:

```go
func newAltScreenView(content string) tea.View {
    v := tea.NewView(content)
    v.AltScreen = true
    return v
}
```

bubbletea v2 diffs `AltScreen` between frames and toggles terminal mode on any mismatch, so an inconsistent branch would flicker the terminal in and out of the alternate buffer. `printFinalFrame` compensates for the other side effect of alt-screen — the pre-launch screen content is restored on exit, which would otherwise make the completed run's summary vanish instead of landing in scrollback:

```go
func printFinalFrame(finalModel tea.Model) {
    if fv, ok := finalModel.(interface{ FullView() string }); ok {
        fmt.Print(fv.FullView())
        return
    }
    fmt.Print(finalModel.View().Content)
}
```

Scrolling models implement `FullView` so the reprint is the complete, unclipped body rather than `View()`'s scrolled window — the whole point of the reprint is the completed-run summary, and PR/run mode auto-quit wherever the user last scrolled (issue #554).

A related width-calculation mitigation lives in `stripVariationSelectors` (`display.go:159-166`): PR titles and run titles are stripped of U+FE0E/U+FE0F variation selectors at ingestion, because those codepoints can make gh-observer's width calculation disagree with bubbletea's grapheme handling and produce off-by-one column errors during partial-line redraw.

---

## 5. TUI Message Processing Loop

Bubbletea follows the Elm Architecture pattern: **Model → Update → View** loop.

### Initialization (`internal/tui/update.go:64-79`)

```go
func (m Model) Init() tea.Cmd {
    cmds := []tea.Cmd{
        m.spinner.Tick,
        fetchPRInfo(m.ctx, m.token, m.host, m.owner, m.repo, m.prNumber),
        tick(m.refreshInterval),
    }
    // One-shot host capability probe (issue #442): check that the Copilot
    // reviewer app exists on this host before arming any Copilot state.
    if m.waitForCopilot {
        cmds = append(cmds, fetchCopilotProbe(m.ctx, m.token, m.host))
    }
    return tea.Batch(cmds...)
}
```

Returns three or four commands that run concurrently:

1. **Spinner tick**: Animates the loading indicator
2. **PR info fetch**: Gets PR title and head SHA (REST API call)
3. **Tick timer**: Schedules periodic polling
4. **Copilot host probe** (when `wait_for_copilot`): Resolves whether the Copilot reviewer app exists on the host at all

### Message Types (`internal/tui/messages.go`)

```go
type TickMsg time.Time              // Poll timer fired

type PRInfoMsg struct {             // PR metadata received
    Number  int
    Title   string
    HeadSHA string
    Err     error
}

type ChecksUpdateMsg struct {        // Check runs updated
    CheckRuns          []ghclient.CheckRunInfo
    CycleStartTime     time.Time     // max(push, PR creation) anchor (issue #518)
    RateLimitRemaining int
    Err                error
}

type WorkflowsDiscoveredMsg struct {  // Workflow discovery complete
    NewRunIDToWorkflowID map[int64]int64
    WorkflowIDsToFetch   []int64
    Err                  error
}

type JobAveragesPartialMsg struct {   // Partial history for single workflow
    WorkflowID int64
    Averages   map[string]time.Duration
    Err        error
}

type CopilotReviewMsg struct {        // Copilot review state for HEAD
    State              string
    Stale              bool
    Pending            bool
    NotRequested       bool
    RateLimitRemaining int
    Err                error
}

type CopilotProbeMsg struct {         // One-shot host capability result
    Capable bool
}

type ErrorMsg struct {                // Error occurred
    Err error
}
```

`PRInfoMsg` no longer carries timing fields: the head-commit REST fetch was removed (issue #349) and the timing anchor arrives via `ChecksUpdateMsg.CycleStartTime` from the GraphQL check-runs query, which selects `pushedDate` and PR `createdAt` in the same round-trip and combines them (issue #518).

### The Update Wrapper & Viewport Sync (`internal/tui/update.go:91-102`)

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    model, cmd := m.update(msg)
    switch v := model.(type) {
    case Model:
        v.syncViewport()
        return v, cmd
    case *Model:
        v.syncViewport()
        return v, cmd
    }
    return model, cmd
}
```

Every message path funnels through `Update`, which dispatches to the internal `update()` switch and then syncs the viewport on whatever model comes back — centralizing the sync means no message path can forget it, and the body the viewport bakes always reflects post-message state (issue #554). The value/pointer split matters: handlers with pointer receivers return `*Model` while inline paths return `Model` by value. `syncViewport` is a no-op until the first `tea.WindowSizeMsg` arrives. `RunModel` and `RepoModel` have identical wrappers.

### Completion Gate: `canTrustCompletion()` (`update.go:18-61`)

`canTrustCompletion` prevents premature exit when fast checks (e.g., DCO) complete before slow checks have appeared in the API response (issue #236):

```go
func canTrustCompletion(m *Model) bool {
    if m.firstCheckSeenAt.IsZero() {
        return false
    }

    checkCount := len(m.checkRuns)

    // --quick mode: no history fetch, so trust peak tracking alone.
    if m.noAvg {
        return m.peakCheckCount <= checkCount
    }

    elapsed := time.Since(m.firstCheckSeenAt)

    // After grace period, trust completion regardless
    if elapsed >= startupGracePeriod {
        return true
    }

    // If checks disappeared (current < peak), don't trust
    if m.peakCheckCount > checkCount {
        return false
    }

    // If we have expected count from history, check appearance ratio
    if m.expectedCheckCount > 0 {
        ratio := float64(checkCount) / float64(m.expectedCheckCount)
        if ratio >= minCheckAppearanceRatio {
            return true
        }
        return false
    }

    // No expected count and grace period not elapsed
    return false
}
```

**Four-tier trust logic**:

1. **Quick mode** (`--quick` / `-q`): Trust completion as soon as the visible count reaches the peak count seen so far. History isn't fetched in quick mode, so there's no `expectedCheckCount` to compare against.
2. **Grace period elapsed** (`startupGracePeriod` = 2 minutes): Always trust completion
3. **Appearance ratio met** (`minCheckAppearanceRatio` = 30%): Trust if we've seen enough of the expected checks — `expectedCheckCount` is derived from `len(m.jobAverages)` after each `JobAveragesPartialMsg`, since each job name in the historical averages represents a check that should eventually appear
4. **Checks disappearing** (current count < peak): Never trust — some checks vanished from the API

### handleChecksUpdate (`internal/tui/update.go:449-600`)

The check update logic includes streaming historical average fetching, presumed averages, and premature exit prevention:

```go
func (m *Model) handleChecksUpdate(msg ChecksUpdateMsg) (tea.Model, tea.Cmd) {
    if msg.Err != nil {
        m.err = msg.Err
        return m, nil  // Continue polling on network errors
    }

    m.checkRuns = msg.CheckRuns
    SortCheckRuns(m.checkRuns)  // Sort by duration
    // Adopt the GraphQL-sourced cycle start anchor on the first successful
    // poll where it is non-zero; preserve the last known value otherwise.
    if !msg.CycleStartTime.IsZero() {
        firstAnchor := m.cycleStartTime.IsZero()
        m.cycleStartTime = msg.CycleStartTime
        // Re-anchor the Copilot wait budget to the real cycle start so
        // copilot_max_wait means "since the CI cycle began", not "since
        // gh-observer attached" (issue #518) — and skip the initial delay
        // when the cycle is already older than the delay window.
        if firstAnchor && m.waitForCopilot && !m.copilotWaitStartTime.IsZero() && m.copilotLastPoll.IsZero() {
            m.copilotWaitStartTime = msg.CycleStartTime
            if time.Since(msg.CycleStartTime) > m.copilotInitialDelay {
                m.copilotPollStartTime = time.Now()
            } else {
                m.copilotPollStartTime = msg.CycleStartTime.Add(m.copilotInitialDelay)
            }
        }
    }
    m.rateLimitRemaining = msg.RateLimitRemaining
    m.fetchReceived = true
    m.lastUpdate = time.Now()
    m.err = nil

    // Inject presumed durations for external GitHub App checks (e.g. DCO).
    // Idempotent — real history fetched later always wins.
    ghclient.ApplyPresumedAverages(m.jobAverages, m.checkRuns, m.presumedAverages, m.host)

    if len(msg.CheckRuns) > m.peakCheckCount {
        m.peakCheckCount = len(msg.CheckRuns)  // Track peak for canTrustCompletion
    }

    if m.firstCheckSeenAt.IsZero() && len(msg.CheckRuns) > 0 {
        m.firstCheckSeenAt = time.Now()
    }

    newChecks := hasNewChecks(msg.CheckRuns, m.seenCheckKeys)
    markChecksSeen(msg.CheckRuns, m.seenCheckKeys)

    var cmds []tea.Cmd

    // Re-discover workflows when new jobs appear after the first cycle
    reDiscover := newChecks && m.historyFetchCompleted && !m.avgFetchPending &&
        m.rateLimitRemaining >= minRateLimitForFetch
    if reDiscover {
        m.avgFetchPending = true
        cmds = append(cmds, discoverWorkflows(...))
    }

    // Fetch historical averages after delay or when checks complete,
    // but only if discovery is actually needed (scan for unknown workflow IDs)
    allComplete := allChecksComplete(msg.CheckRuns)
    elapsed := time.Since(m.firstCheckSeenAt)
    readyForHistory := !m.noAvg && !m.firstCheckSeenAt.IsZero() &&
        (allComplete || elapsed >= historyFetchDelay)
    // ... needsDiscovery scan + AdvSec matching, then:

    if allChecksComplete(m.checkRuns) && canTrustCompletion(m) && copilotGateSatisfied(m) {
        m.exitCode = determineExitCode(m.checkRuns, m.copilotState, m.waitForCopilot)
        m.checksComplete = true
        // Only quit if no pending/dispatched workflow fetches
        if !m.avgFetchPending && len(m.pendingWorkflowFetch) == 0 {
            m.quitting = true
            cmds = append(cmds, tea.Quit)
        }
        return m, tea.Batch(cmds...)
    }

    return m, tea.Batch(cmds...)
}
```

**Key behaviors**:

1. **Delayed history fetch**: Waits `historyFetchDelay` (10s) after the first check appears before fetching history, or immediately when all checks are already complete
2. **Streaming discovery**: Uses `discoverWorkflows()` to find workflow IDs, then dispatches individual `fetchWorkflowHistory()` calls; a `needsDiscovery` pre-scan skips the round trip when every check's workflow is already known
3. **Pending tracking**: Tracks `pendingWorkflowFetch` and `dispatchedWorkflowFetch` maps to coordinate concurrent fetches
4. **Re-discovery**: New check keys (`checkKey`: run ID / details URL / name) after a completed discovery cycle trigger a fresh one — matrix-expanding jobs (new jobs appearing as earlier ones complete) get their averages too
5. **Exit coordination**: Waits for all workflow fetches to complete before quitting, and additionally requires `copilotGateSatisfied` so the program can't quit while a Copilot review is still pending
6. **Premature exit prevention**: `canTrustCompletion()` gates the exit decision; when untrusted, the view shows "Waiting for more checks to appear..." instead

### Copilot Code Review Tracking (issues #409, #442, #486)

Copilot code reviews live in `PullRequest.reviews` — not `StatusCheckRollup.Contexts` — so without a separate fetch path, gh-observer would quit "green" while Copilot is still reviewing. When `wait_for_copilot` is enabled (default), PR mode runs a parallel detection pipeline:

#### Gate arming (`PRInfoMsg` handler, `update.go:186-258`)

Once the head SHA is known, the gate arms (unless the host probe already found the host incapable):

- `copilotPending = true` — exit is blocked until the review resolves
- `copilotWaitStartTime = now` — provisional fallback anchor for the `copilot_max_wait` budget
- `copilotPollStartTime = now + copilotInitialDelay` — the first poll may fire only after this instant, giving GitHub time to create the review request after a push

The first poll is **not** dispatched here; the TickMsg path fires it once the delay window elapses. `handleChecksUpdate` later re-anchors both timestamps to the real cycle start (`m.cycleStartTime` — the later of push and PR creation, issue #518) so `copilot_max_wait` measures "since the CI cycle began" like queue latency. When the cycle already predates the initial-delay window (e.g. attaching to an old PR), the delay is skipped — any review request already exists by then, or never will.

#### Host capability probe (`CopilotProbeMsg`, `copilot_probe.go`)

`fetchCopilotProbe` runs once from `Init`. `CopilotReviewerExistsOnHost` checks whether the Copilot reviewer GitHub App exists on the host at all:

- **github.com short-circuits to true** without a request — the app is a first-party installation there, so a flaky response can never disable detection for public-host users
- **Enterprise hosts** GET `/users/copilot-pull-request-reviewer[bot]` with a 10s timeout: a definitive 404 means Copilot code review cannot exist there (e.g. GHES without it); any other error (5xx, network, auth) conservatively reports true — the probe must only disable detection on a definitive "does not exist", never on a flaky network

An incapable result (`CopilotProbeMsg{Capable: false}`) disarms the gate entirely: state is cleared, the poll times are zeroed, the synthetic row and status line never render, and — since the gate may have been the only thing holding exit open — the completion conditions are re-checked so the program exits if checks already finished.

#### Poll cadence (`TickMsg` handler, `update.go:124-184`)

On each tick, a Copilot poll dispatches when all of these hold:

- the gate is armed (`copilotPending`), the program isn't quitting, and the host is capable (or the probe hasn't answered yet — armed optimistically)
- the rate limit is at or above `minRateLimitForFetch`
- the initial-delay window has elapsed (`copilotPollStartTime` is in the past)
- at least `copilotPollInterval` (10s default) has passed since the last poll

The same TickMsg handler first checks the **max-wait timeout** (see below) before the rate-limit backoff early-return — the timeout is a pure local state transition with no API cost, so it must never be starved by backoff (the #442 report of a review spinning "in progress" for 192h was exactly this starvation).

#### Review resolution (`handleCopilotReview`, `update.go:656-722`)

`CopilotReviewMsg` processing implements a two-consecutive-not-requested rule:

- **Not requested, no stale review**: Increment `copilotNotReqStreak`; only after **two consecutive** such polls is Copilot declared absent. One poll alone can race GraphQL read lag after the REST POST that requested the review.
- **Any other state** (request, review, or stale review): positive evidence — `copilotEvidenceSeen` is set, which is what makes the synthetic row visible. Pending keeps the gate armed; a submitted review or a stale one clears it (`copilotReviewComplete = true`).
- If checks are already complete and history has drained, the quit fires right from this handler instead of waiting for the next poll.

The message's rate-limit observation is folded into the model's accounting (minimum), so the separate Copilot query participates in backoff.

#### The give-up timeout (issue #442)

Two independent mechanisms bound the wait so a never-completing review can't pin the program forever:

1. **`copilotGateSatisfied`** (`update.go:606-629`) — the exit gate: returns true when the feature is off, the review isn't pending, the review is stale (informational, self-corrects), or `copilot_max_wait` has elapsed since `copilotWaitStartTime`
2. **The TickMsg timeout** — the display/poll bound: once the budget is spent and at least one poll has resolved (or polls are suppressed by a genuinely low quota), `copilotPending` clears and `copilotTimedOut` is set — but only when Copilot was actually seen: "giving up" is meaningful for a request that never completed, while a repo without Copilot resolves silently instead of reproducing the #442 confusion

A timed-out row renders as a completed "⏱" row with a "didn't complete within 3m — giving up" summary.

#### Determining exit eligibility

```go
if allChecksComplete(m.checkRuns) && canTrustCompletion(m) && copilotGateSatisfied(m) {
    m.exitCode = determineExitCode(m.checkRuns, m.copilotState, m.waitForCopilot)
    ...
}
```

The Copilot gate is deliberately **not** part of `allChecksComplete` — the synthetic review row never enters `m.checkRuns`, so sorting, timing, and completion logic remain untouched. It's a separate conjunct at the single call site that decides exit.

### Check Sorting (`internal/tui/display.go:337-387`)

```go
func SortCheckRuns(checks []ghclient.CheckRunInfo) {
    sort.Slice(checks, func(i, j int) bool {
        di := sortKeyDuration(checks[i])
        dj := sortKeyDuration(checks[j])
        if di != dj {
            return di < dj  // Shortest duration first
        }
        si := statusPriority(checks[i].Status)
        sj := statusPriority(checks[j].Status)
        if si != sj {
            return si < sj  // in_progress > completed > queued
        }
        return FormatCheckName(checks[i]) < FormatCheckName(checks[j])
    })
}
```

**Sorting Priority**: duration (shortest first) → status (in_progress first) → name (alphabetical). Queued jobs get a huge sort key (`1 << 62`) so they sink to the bottom.

### Run-mode message loop (`internal/tui/runupdate.go`)

`RunModel.Update` mirrors the PR-mode flow (including the viewport-sync wrapper) but with run-specific messages:

- **`RunInfoMsg`** — run metadata (title, head SHA, push time from the GraphQL `pushedDate` lookup, rate limit); triggers the first `fetchRunJobs`
- **`RunJobsUpdateMsg`** — updated `[]WorkflowJobInfo` plus `RateLimitRemaining`
- **`RunWorkflowsDiscoveredMsg` / `RunJobAveragesPartialMsg`** — run-mode twins of the PR-mode messages; both modes reuse the same `DiscoverWorkflows` → `FetchWorkflowHistory` pipeline
- **`RunTickMsg`** — same rate-limit backoff (triples interval when `rateLimitRemaining < rateBackoffThreshold`), gated on `fetchReceived` so the pre-first-response zero value doesn't suppress fetches

Run mode has no `canTrustCompletion` machinery (jobs come from a bounded, per-run API list — there's no "more jobs may appear" ambiguity) but does keep a `seenJobKeys` map so discovery is only retriggered when new job names appear, not on every poll. Completion uses `ghclient.AllJobsComplete` and `ghclient.DetermineRunExitCode`. Presumed averages are applied on each jobs update for consistency with PR mode.

### Exit Code Determination (`internal/tui/update.go:857-867`)

```go
func determineExitCode(checks []ghclient.CheckRunInfo, copilotState string, waitForCopilot bool) int {
    for _, check := range checks {
        if ghclient.FailureConclusion(check.Conclusion) {
            return 1
        }
    }
    if waitForCopilot && ghclient.CopilotReviewFails(copilotState) {
        return 1
    }
    return 0
}
```

Uses `FailureConclusion()` from `internal/github/conclusion.go` to check for failure states, plus `CopilotReviewFails()` (changes_requested) when Copilot detection is enabled. The `copilotState`/`waitForCopilot` parameters are only meaningful in PR mode; run mode passes `""` and `false`. Run mode's equivalent `DetermineRunExitCode()` uses `FailureJobConclusion()` to match the Actions jobs API conclusion vocabulary.

---

## 6. GitHub API Layer Deep Dive

### GraphQL Query Structure (`internal/github/graphql.go:116-143`)

The GraphQL query mirrors the structure used by `gh pr checks`, with cursor pagination for PRs with more than 100 status contexts and the timing fields folded into the same round-trip:

```graphql
query($owner: String!, $repo: String!, $prNumber: Int!, $contextsCursor: String) {
    repository(owner: $owner, name: $repo) {
        pullRequest(number: $prNumber) {
            createdAt
            commits(last: 1) {
                nodes {
                    commit {
                        pushedDate
                        committedDate
                        statusCheckRollup {
                            contexts(first: 100, after: $contextsCursor) {
                                nodes {
                                    __typename
                                    ... on CheckRun {
                                        name
                                        summary
                                        status
                                        conclusion
                                        startedAt
                                        completedAt
                                        detailsUrl
                                        annotations(first: 5) {
                                            nodes {
                                                message
                                                path
                                                title
                                                annotationLevel
                                                location { start { line } }
                                            }
                                        }
                                        checkSuite {
                                            workflowRun {
                                                databaseId
                                                workflow { databaseId, name }
                                            }
                                            app {
                                                name
                                                slug
                                            }
                                        }
                                    }
                                    ... on StatusContext {
                                        context
                                        description
                                        state
                                        targetUrl
                                    }
                                }
                                pageInfo { hasNextPage, endCursor }
                            }
                        }
                    }
                }
            }
        }
    }
    rateLimit { limit, remaining }
}
```

**Workflow ID shortcuts**: The `databaseId` fields on `workflowRun` and `workflow` (parsed through a custom `BigInt` JSON unmarshaller) let the history pipeline resolve run→workflow mappings without extra REST round trips.

**App Name Detection**: The `checkSuite.app` field detects GitHub Advanced Security (GHAS) checks and third-party apps (like Checkov) that don't have a `workflowRun`. The `AppName` field in `CheckRunInfo` stores this value, and the display layer uses it as a fallback prefix when `WorkflowName` is empty — so "analyze" from GitHub Code Scanning renders as "GitHub Code Scanning / analyze" instead of just "analyze".

**Rate limit**: The query requests `rateLimit { limit, remaining }`; every page's observation is normalized (see [Rate Limit Normalization](#rate-limit-normalization-internalgithubclientgo26-47)) and the minimum is kept across pages.

### CheckRunInfo Structure (`internal/github/graphql.go:52-67`)

```go
type CheckRunInfo struct {
    Name          string
    WorkflowName  string    // From checkSuite.workflowRun.workflow.name
    AppName       string    // From checkSuite.app.name (GHAS, third-party apps)
    Summary       string
    Status        string
    Conclusion    string
    StartedAt     *time.Time
    CompletedAt   *time.Time
    DetailsURL    string
    Annotations   []Annotation
    WorkflowRunID int64     // From checkSuite.workflowRun.databaseId
    WorkflowID    int64     // From checkSuite.workflow.workflow.databaseId
    Kind          string    // "" = real check run; "review" = synthetic Copilot row
    ReviewState   string    // Copilot review vocabulary, when Kind == "review"
}
```

`Kind` discriminates the row type: the zero value is a regular check run sourced from GraphQL/REST, while `"review"` (constant `copilotRowKind` in display.go) denotes a synthetic Copilot review row. Synthetic rows carry review state in `ReviewState` — intentionally distinct from `Status`/`Conclusion` so timing and conclusion helpers remain untouched, and they never participate in timing calculations or exit-code checks (they never enter `m.checkRuns`).

Legacy commit-status contexts (`... on StatusContext`) are converted into the same shape: `success` → completed/success, `error`/`failure` → completed/failure, `pending` → queued.

### Cycle Start Anchor (`internal/github/graphql.go:245-250`)

```go
func cycleStart(pushTime, prCreatedAt time.Time) time.Time {
    if pushTime.IsZero() || prCreatedAt.After(pushTime) {
        return prCreatedAt
    }
    return pushTime
}
```

The effective cycle-start anchor is the **later** of the head commit's push time and the PR's creation time (issue #518). The push time alone can predate the current CI cycle — a PR opened from an already-pushed branch, or a `committedDate` fallback dragged stale by a rebase — making every "since push" display read misleadingly old. Creation time clamps that stale anchor; a force-push after PR creation keeps the push time. This anchor backs the queue-latency column, the "Started Xs ago" header, and the Copilot wait budget.

### PR Metadata Fetching (`internal/github/pr.go:156-170`)

```go
func FetchPRInfo(ctx context.Context, client *github.Client, owner, repo string, prNumber int) (*PRInfo, error) {
    pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)

    return &PRInfo{
        Number:    prNumber,
        Title:     pr.GetTitle(),
        HeadSHA:   headSHA,
        CreatedAt: pr.GetCreatedAt().Format(TimestampFormat),
    }, nil
}
```

Only PR-level fields come from REST; the head commit's push time is sourced from the GraphQL check-runs query, which fetches `pushedDate` in the same round-trip. The REST commit endpoint only exposes committer/author timestamps, not `pushedDate`, so it is not used (issue #349). `TimestampFormat` comes from `internal/github/timestamp.go`.

### URL Parsing (`internal/github/pr.go:23-148`)

`ParsePRURL` and `ParseActionsRunURL` extract **host, owner, repo, and number/ID** from URLs. The host segment accepts any hostname (GitHub Enterprise support, issue #479), lowercases it, and requires it to start and end alphanumeric; hosts with ports are a documented limitation. Owner/repo segments are restricted to GitHub slug characters (looser matching let fuzzing find inputs like `https://github.com/ow ner/re po/pull/1` — rejected now). PR numbers and run IDs of 0 are rejected (both fuzz findings).

### Forked Repository and jj Handling (`internal/github/pr.go:46-109`)

**Problem**: When working on a forked repo, `git remote get-url origin` returns the fork's URL, not the upstream repository where the PR lives. Additionally, in jj (Jujutsu) non-colocated repos, `gh pr view` cannot find the git repository.

**Solution**: Use `gh pr view --json number,url` to get the PR URL, then extract host/owner/repo from that URL. In jj repos, set `GIT_DIR` from `jj git root` so `gh pr view` can locate the git directory:

```go
func GetCurrentPRWithRepo() (int, string, string, string, error) {
    cmd := exec.Command("gh", "pr", "view", "--json", "number,url")
    SetGITDirForJJ(cmd) // Sets GIT_DIR for jj compatibility
    output, err := cmd.Output()
    return parsePRViewWithRepo(output)
}

func parsePRViewWithRepo(jsonOutput []byte) (int, string, string, string, error) {
    // Parse JSON, then extract host/owner/repo from the URL like
    // https://github.com/owner/repo/pull/123 — so an enterprise checkout
    // resolves against the enterprise instance, and a fork resolves upstream.
    host, owner, repo, prNum, err := ParsePRURL(result.URL)
    return result.Number, host, owner, repo, nil
}
```

### Jujutsu (jj) VCS Compatibility (`internal/github/detect.go`)

The application detects jj repos and adapts its behavior:

- **`IsJujutsu()`** - Searches upward from cwd for a `.jj/` directory, caching the result (sync.Once)
- **`SetGITDirForJJ(cmd)`** - When a jj repo is detected, runs `jj git root` to find the internal git directory path, then sets `GIT_DIR` on the `exec.Cmd` so `gh pr view` works in non-colocated jj workspaces
- **Error messaging** - When auto-detection fails in a jj repo, `parseArgs` suggests the explicit forms (PR number, PR URL) or `jj git colocation enable`

This follows the approach recommended in jj's documentation: `GIT_DIR=$(jj git root) gh pr view ...`

### Copilot Review Queries (`internal/github/reviews.go`)

`FetchCopilotReview` runs a second GraphQL path alongside the check-runs query, hitting `PullRequest.reviewRequests` and `PullRequest.reviews`. GitHub silently drops Bot reviewers from the REST `requested_reviewers` endpoint (issue #288), so GraphQL with `... on Bot` / `... on User` inline fragments is required to see Copilot's request at all.

Both connections are cursor-paginated (issue #486) so a PR with deep review history cannot hide Copilot state:

- `reviewRequests(first: 10, after: $requestsCursor)` walks **forward** until the Copilot request is found or the connection is exhausted
- `reviews(last: 25, before: $reviewsCursor)` walks **backward** (newest pages first, prepended to keep ascending submission order) until a Copilot review targeting HEAD is found or the connection is exhausted
- The walk is bounded by `maxCopilotQueryPages` (10 pages ≈ 250 reviews / 100 reviewers) so a stale review deep in history can't drive unbounded API traffic on every poll cycle
- A query failure discards partial state on purpose: pages gathered before the failure are an incomplete window, and the next poll refetches from page 1

`parseCopilotReview` then resolves the state:

1. **Newest Copilot review targeting HEAD** → its state (approved / changes_requested / commented / dismissed; `PENDING` maps to in-progress)
2. **Review requested, nothing submitted on HEAD** → pending (a fresh request takes precedence over stale older reviews)
3. **Only stale reviews** (different commit OID) → `Stale: true`, surfaced as a "refresh or re-request" warning
4. **Nothing at all** → not requested

### Historical Job Averages (`internal/github/history.go`)

The application uses a **streaming approach** to fetch historical averages efficiently, with a recency-weighted mean:

**Weighted average** (`historyDecayFactor = 0.7`): Each run contributes `0.7^i` of the average (run 0 = newest), so the newest run is ~31% of a 10-run average and a slow run from two weeks ago no longer drags the ETA above where the next run actually lands. The durations slices must be newest-first — `averageJobDurations` documents and preserves this ordering invariant (with a known nondeterminism caveat in the legacy snapshot-mode path when two workflows share a bare job name).

**Streaming functions**:

```go
// DiscoverWorkflows resolves run IDs to workflow IDs, preferring the
// WorkflowRunID/WorkflowID fields GraphQL already returned and falling
// back to a REST GetWorkflowRunByID call (with caching) for checks that
// only carry a details URL.
func DiscoverWorkflows(
    ctx context.Context, client *github.Client, owner, repo string,
    checkRuns []CheckRunInfo,
    knownRunIDToWorkflowID map[int64]int64,
    knownFetchedWorkflowIDs map[int64]bool,
) (newRunIDToWorkflowID map[int64]int64, workflowIDsToFetch []int64, err error)

// FetchWorkflowHistory fetches historical job durations for a single workflow.
func FetchWorkflowHistory(
    ctx context.Context, client *github.Client, owner, repo string,
    workflowID int64,
) (map[string]time.Duration, error)
```

**Streaming Flow** (`internal/tui/update.go`):

1. `handleChecksUpdate()` detects checks have arrived
2. Waits for `historyFetchDelay` (10s) after first checks appear (or proceeds immediately once all checks are complete)
3. Dispatches `discoverWorkflows()` — skipped when a pre-scan shows every workflow is already known
4. `WorkflowsDiscoveredMsg` returns workflow IDs to fetch
5. For each workflow ID, dispatches `fetchWorkflowHistory()`; `pendingWorkflowFetch`/`dispatchedWorkflowFetch` maps prevent redundant dispatches across polls
6. Each `JobAveragesPartialMsg` merges results incrementally and updates `expectedCheckCount = len(m.jobAverages)`
7. When `pendingWorkflowFetch` is empty, the discovery phase completes (`historyFetchCompleted`); later polls with **new** check keys can trigger a fresh discovery cycle (matrix jobs that appear as earlier ones complete)

**AdvSec aliasing** (`DiscoverAdvSecWorkflows`): GitHub Advanced Security checks (CodeQL, Checkov) have `workflowRun: null`, but their Name typically matches the WorkflowName of a real Actions workflow in the same PR. When a name match is found, the workflow's fetched averages are aliased into `jobAverages` under the AdvSec check's name (using the first job's duration), and the workflow is queued for fetching if not already cached. The `workflowAverages` map caches per-workflow results so aliases can be written as soon as the match and the fetch both exist.

**Presumed averages** (`ApplyPresumedAverages`, `IsExternalAppCheck`): External GitHub App checks like DCO (Probot) have no Actions workflow run to fetch history from — their details URL points off-site. For each check in the configured `presumed_averages` map (default: `DCO: 1s`) that appears as an external app check, the configured duration is written into `jobAverages`. Matching is case-insensitive (viper lowercases config map keys), it's idempotent, and real history fetched later always wins. GitHub-hosted URLs on the watched host don't count as external, so AdvSec aliasing isn't blocked by a presumed average having already taken the slot.

**Legacy Function** (`FetchJobAverages`): The non-streaming variant still backs snapshot mode — extract run IDs from check URLs (or use the GraphQL-provided IDs directly), map to workflow IDs with caching, list 10 recent completed runs per workflow, collect job durations, and weighted-average per job name.

### Actions Run Metadata (`internal/github/runs.go`)

`FetchRunInfo` resolves the "Pushed Xs ago" header: the REST run object only exposes `head_commit.timestamp` (the committer date), so a one-shot GraphQL `Repository.object(oid:)` lookup fetches the real `pushedDate` (with `committedDate` fallback). Failure falls back to the REST timestamp — the header still renders, just slightly older (issue #349). `FetchRunJobs` paginates through all jobs (`PerPage: 100`), and `FailureJobConclusion` / `AllJobsComplete` / `DetermineRunExitCode` are the run-mode equivalents of the PR-mode conclusion helpers.

### Helper Modules

#### `internal/github/conclusion.go`

```go
func FailureConclusion(conclusion string) bool {
    return conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required"
}
```

Simple helper that centralizes failure conclusion logic. Run mode's `FailureJobConclusion` (in `runs.go`) matches the same vocabulary.

#### `internal/github/timestamp.go`

```go
const TimestampFormat = "2006-01-02T15:04:05Z"

func ParseTimestamp(s string) (time.Time, error) {
    parsed, err := time.Parse(TimestampFormat, s)
    if err != nil {
        return time.Time{}, err
    }
    if parsed.Format(TimestampFormat) != s {
        return time.Time{}, fmt.Errorf("timestamp %q does not match format %q exactly", s, TimestampFormat)
    }
    return parsed, nil
}
```

Centralized timestamp format for parsing GitHub API timestamps. The explicit round-trip check exists because `time.Parse` silently accepts (and drops) fractional seconds the layout doesn't describe — a fuzz finding.

#### `internal/github/client.go`

- `safeGraphQLInt` bounds-checks `int` → `githubv4.Int` (int32) conversions, preventing silent truncation on 64-bit platforms
- `UnknownRateLimit` / `normalizeRateLimit` — see [Rate Limit Normalization](#rate-limit-normalization-internalgithubclientgo26-47)

---

## 7. Timing Calculations

The `internal/timing/calculator.go` module provides three core metrics. All of them operate on real check runs only — synthetic Copilot review rows never participate, since reviews expose no started/completed timestamps.

### Queue Latency (`calculator.go:21-26`)

```go
func QueueLatency(cycleStart time.Time, check ghclient.CheckRunInfo) time.Duration {
    if check.StartedAt == nil || cycleStart.IsZero() {
        return 0
    }
    return check.StartedAt.Sub(cycleStart)
}
```

**Measures**: Time from cycle start to check start. The anchor is the later of the head commit's `pushedDate` and the PR's creation time (issue #518), not the commit-author time: a PR opened from an already-pushed branch would otherwise show queue times that predate the cycle. A negative result (check started before the anchor) lands in the caller's placeholder branch.

### Runtime (`calculator.go:29-34`)

```go
func Runtime(check ghclient.CheckRunInfo) time.Duration {
    if check.Status != "in_progress" || check.StartedAt == nil {
        return 0
    }
    return time.Since(*check.StartedAt)
}
```

**Measures**: Elapsed time for currently running checks.

### Final Duration (`calculator.go:37-42`)

```go
func FinalDuration(check ghclient.CheckRunInfo) time.Duration {
    if check.StartedAt == nil || check.CompletedAt == nil {
        return 0
    }
    return check.CompletedAt.Sub(*check.StartedAt)
}
```

**Measures**: Total runtime for completed checks.

### Duration Formatting (`calculator.go:60-79`)

`FormatDuration` rounds to the nearest second and **always renders all smaller units down to seconds**, so durations don't flicker between width-changing forms as they tick (issue #314):

```go
func FormatDuration(d time.Duration) string {
    d = d.Round(time.Second)
    if d <= 0 {
        return "0s"
    }
    hours := int(d / time.Hour)
    d -= time.Duration(hours) * time.Hour
    minutes := int(d / time.Minute)
    d -= time.Duration(minutes) * time.Minute
    seconds := int(d / time.Second)
    if hours > 0 {
        return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
    }
    if minutes > 0 {
        return fmt.Sprintf("%dm %ds", minutes, seconds)
    }
    return fmt.Sprintf("%ds", seconds)
}
```

**Output Examples** (after the #314 change):

- 45s → `45s`
- 60s → `1m 0s` (was `1m`)
- 125s → `2m 5s`
- 3600s → `1h 0m 0s` (was `1h`)
- 3725s → `1h 2m 5s`

The two run-mode helpers `RunJobRuntime` (`calculator.go:45-50`) and `RunJobDuration` (`calculator.go:53-58`) compute durations from job timestamps; the formatted output still flows through `FormatDuration`.

---

## 8. TUI Rendering System

### Constants (`internal/tui/constants.go`)

```go
const (
    slowJobThreshold     = 2 * time.Minute
    verySlowJobThreshold = 3 * time.Minute

    rateBackoffThreshold = 10
    rateWarningThreshold = 500
    minRateLimitForFetch = 100

    historyFetchDelay = 10 * time.Second

    minCheckAppearanceRatio = 0.3
    startupGracePeriod      = 2 * time.Minute
)
```

**Threshold Explanations**:

- `slowJobThreshold`: Time before showing "Still waiting" message
- `verySlowJobThreshold`: Time before showing "No checks found" message
- `rateBackoffThreshold`: Remaining API calls before tripling poll interval
- `rateWarningThreshold`: Remaining API calls before showing a yellow rate-limit indicator
- `minRateLimitForFetch`: Minimum rate limit to fetch historical averages (and dispatch Copilot polls)
- `historyFetchDelay`: Delay before starting historical average fetch (prevents premature API calls during check startup)
- `minCheckAppearanceRatio`: Minimum ratio of seen checks to expected checks (30%) before trusting completion
- `startupGracePeriod`: Maximum time to wait before trusting completion regardless of check counts (2 minutes)

The Copilot timing knobs (`copilot_max_wait`, `copilot_poll_interval`, `copilot_initial_delay`) are **config** values, not constants — see [Step 3 of Section 1](#step-3-configuration-loading-maingo150-155).

### View Function (`internal/tui/view.go:19-39`)

Each mode renders three sections: a pinned header, a scrollable body, and a pinned footer. Before the first `tea.WindowSizeMsg` arrives (height == 0) all sections render unclipped in a single string; once the terminal size is known, the body scrolls inside a viewport between the pinned sections (issue #554):

```go
func (m Model) View() tea.View {
    if m.err != nil {
        return newAltScreenView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
    }
    if m.height == 0 {
        return newAltScreenView(m.renderHeader() + m.renderBody() + m.renderFooter())
    }
    return newAltScreenView(m.composeView(m.renderHeader(), strings.TrimSuffix(m.renderFooter(), "\n")))
}

// FullView returns the complete, unclipped render. printFinalFrame uses it
// at quit time so the completed-run summary lands intact in scrollback.
func (m Model) FullView() string {
    if m.err != nil {
        return m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err))
    }
    return m.renderHeader() + m.renderBody() + m.renderFooter()
}
```

`renderHeader` (PR mode) shows the PR title, UTC clock, "Updated / Started Xs ago", and the historical-averages status (fetching with elapsed, unavailable on error, or ready with workflow count and duration). `renderFooter` shows the two-tier rate-limit indicator (red under `minRateLimitForFetch`, yellow under `rateWarningThreshold`, only after `fetchReceived`) and the key hint, with the scroll hints taking over when the body overflows.

`renderBody` renders the checks table (with dimmed summary lines and error-annotation boxes for failures), the synthetic Copilot review row and status line, and the waiting-for-more-checks block:

```go
if allChecksComplete(m.checkRuns) && !canTrustCompletion(&m) {
    b.WriteString(m.styles.Queued.Render("  ⏳ Waiting for more checks to appear...\n"))
    if m.expectedCheckCount > 0 {
        fmt.Fprintf(&b, m.styles.Queued.Render("  Seen %d of ~%d expected checks (%d%% threshold: %d%%)\n"),
            len(m.checkRuns), m.expectedCheckCount,
            int(minCheckAppearanceRatio*100),
            int(float64(len(m.checkRuns))/float64(m.expectedCheckCount)*100))
    } else if m.noAvg {
        b.WriteString(m.styles.Queued.Render("  Waiting for all seen checks to finish...\n"))
    } else {
        // Grace period countdown
    }
}
```

**Premature Exit Display**: When all visible checks are complete but `canTrustCompletion()` returns false, the TUI shows the appearance ratio (when history provided an expected count), a quick-mode message, or a grace period countdown. This prevents the user from seeing a brief "all passed" state followed by new checks appearing.

### Scrolling Subsystem (`internal/tui/scroll.go`)

All three interactive modes embed a shared `scrollState` (issue #554; PR/run modes added in #558, repo mode in #542):

```go
type scrollState struct {
    viewport viewport.Model
    width    int
    height   int
    pendingG bool  // one "g" seen; a second jumps to top (vim "gg")
}
```

- **Keymap**: `newScrollState()` wires a bubbles v2 viewport with PageDown = `pgdown`/`space`/`f`/`ctrl+f`, PageUp = `pgup`/`b`/`ctrl+b`, and the defaults for line (`j`/`k`/arrows) and half-page (`ctrl+d`/`ctrl+u`) movement. Horizontal keys are disabled so left/right are inert. `gg`/`G`/`home`/`end` jump to top/bottom, with `pendingG` tracking the two-keypress `gg` sequence
- **`handleKey`** applies a scroll key to the body viewport — a no-op until the terminal size is known, so keys typed before the first `tea.WindowSizeMsg` don't scroll an unsized viewport
- **`sync(header, body, footer)`** sizes the viewport to the space between the pinned sections and refreshes its content. bubbletea clips long lines instead of wrapping, so each line is one row; empty sections consume zero rows (`lipgloss.Height("")` is 1, so the empty-startup-phase footer must not eat a row)
- **`composeView(header, footer)`** stitches pinned header + viewport + pinned footer into a frame
- **`scrollable()`** reports whether the body overflows the viewport; **`scrollHint()`** renders the vim-style key hints plus a `Top`/`Bot`/`NN%` ruler, dropping hints from the end until the line fits the terminal width

**Footer height stability**: the footer is measured *before* the refreshed content makes `scrollable()` knowable, so a footer whose row count depends on the scroll state would oscillate the viewport size at the overflow boundary. PR and run modes give the scroll hint the plain "Press q to quit" variant's leading blank line so both variants occupy the same row count; repo mode's variants are inherently equal.

### Header Format (`internal/tui/display.go:305-321`)

```go
func FormatHeaderColumns(widths ColumnWidths) (string, string, string, string) {
    headerQueue := strings.Repeat(" ", max(widths.QueueWidth-7, 0)) + "Start"
    headerName := "Workflow/Job" + strings.Repeat(" ", max(widths.NameWidth-12, 0))
    headerDuration := strings.Repeat(" ", max(widths.DurationWidth-7, 0)) + "ThisRun"
    headerAvg := strings.Repeat(" ", max(widths.AvgWidth-7, 0)) + "HistAvg"
    return headerQueue, headerName, headerDuration, headerAvg
}
```

**Column Headers**:

- "Start" - Queue latency
- "Workflow/Job" - Check name (may show "App / Job" for GHAS or third-party checks)
- "ThisRun" - Current run duration
- "HistAvg" - Historical average duration

### Check Name Formatting (`internal/tui/display.go:169-209`)

`FormatCheckName` supports three tiers of name formatting:

```go
func FormatCheckName(check ghclient.CheckRunInfo) string {
    if check.WorkflowName != "" {
        return fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
    }
    if check.AppName != "" {
        return fmt.Sprintf("%s / %s", check.AppName, check.Name)
    }
    return check.Name
}
```

**Display Format** - Check names are shown with the following priority:

1. **Workflow / Job**: For GitHub Actions workflow runs (e.g., "CI / test")
2. **App / Job**: For GitHub Advanced Security or third-party app checks without a workflow (e.g., "GitHub Code Scanning / analyze", "Bridgecrew / Checkov")
3. **Job only**: For legacy checks without workflow or app context (e.g., "Checkov")

`FormatCheckNameWithTruncate` follows the same priority for truncation, preserving the prefix and truncating only the job name. The name column is capped at `maxCheckNameWidth` (60).

### Copilot Row Rendering (`internal/tui/view.go:376-641`)

The synthetic Copilot row reuses the checks-table geometry but is display-only — it never enters `m.checkRuns`, so `SortCheckRuns`, `allChecksComplete`, and `determineExitCode` are unaffected:

- **`buildCopilotCheckRun`** synthesizes a `CheckRunInfo` with `Kind: "review"` and `WorkflowName: "Copilot"`, mapping review state onto a status shape: queued (inside the initial-delay window, duration column counts down "in 15s"), in_progress (elapsed runtime while polling), or completed (terminal states show `-`, since reviews expose no timestamps). It returns nil — no row — when the feature is off, the gate was never armed, the host probe found the host incapable, or no positive evidence has been seen yet (issue #442: repos that never use Copilot see a neutral "Checking for Copilot review…" status line instead of a phantom queued row). Stale and timed-out rows carry a `Summary` line with actionable guidance ("refresh or re-request", "giving up")
- **`GetCopilotReviewIcon`** (`display.go:117-139`) gives each review state its own icon (✓ approved, ✗ changes_requested, 💬 commented, ⊘ dismissed, ⚠ stale, ⏱ timed_out, ⏸/◐ pending-by-status)
- **`copilotDurationText`** is shared between the width calculation (`widenForCopilotRow`) and the row render so the two agree on the exact string — including the "in Xs" countdown that `FormatDuration` alone doesn't synthesize. Without the shared path, a wide countdown would overflow the pre-computed DurationWidth and misalign the row against the header
- **`renderCopilotStatusLine`** renders a spinner + countdown/elapsed line below the row: "queued, polling in 15s…" during the delay window, "Checking for Copilot review…" pre-evidence, and "in progress… (giving up in 2m 30s)" once evidence exists — the give-up countdown communicates the real wait budget instead of a misleading elapsed time

### Error Annotation Display (`internal/tui/view.go:263-296`)

```go
func (m Model) renderErrorBox(check ghclient.CheckRunInfo, widths ColumnWidths) string {
    var b strings.Builder

    for _, ann := range check.Annotations {
        // Compose: [path:line - ]title: message
        // ...
        b.WriteString("  ")
        b.WriteString(m.styles.ErrorBox.Render(errorMsg))
        b.WriteString("\n")
    }
    // ...
}
```

Annotations are fetched directly via GraphQL (first 5 per check) and include path, line number, title, and message. Failed checks also render their `Summary` as a dimmed description line. Repo mode's GraphQL query omits annotations entirely (see [Section 12](#12-repo-mode)) and never renders these boxes.

---

## 9. Error Handling & Edge Cases

### Network Errors During Polling (`internal/tui/update.go:450-453`)

```go
if msg.Err != nil {
    m.err = msg.Err
    return m, nil  // Continue polling, don't quit
}
```

**Design Decision**: Network errors during polling are non-fatal. The TUI displays the error but continues polling. The same applies to the Copilot review fetch — a failed poll keeps the gate armed and the next tick refetches. The exception is the *initial* PR info fetch (and run mode's run info fetch): with nothing to poll for, the model stores the error and quits, and the final frame renders the error — the exit code stays 0, since no checks were ever seen to fail.

### Rate Limit Handling (`internal/tui/update.go:157-164`)

```go
// Gate on fetchReceived so the zero-value rateLimitRemaining (0) before
// the first successful response doesn't suppress the fetch that would
// clear that state.
if m.fetchReceived && m.rateLimitRemaining < rateBackoffThreshold {
    return m, tick(m.refreshInterval * 3)  // Back off to 15s
}
```

**Backoff Strategy**: When remaining API calls < 10, poll interval triples from 5s to 15s. Two guards keep the accounting honest:

- `fetchReceived` gates backoff (and the on-screen indicator) until the first successful response, so the Go zero value (0) can't pin the model in backoff before any request has been made
- `normalizeRateLimit` maps an absent rate-limit observation (GHES with rate limiting disabled) to `UnknownRateLimit` (5000) instead of a spurious "0 remaining" (issue #442)

The TickMsg handler runs the Copilot max-wait timeout check **before** this backoff early-return — the timeout is a pure local state transition with no API cost, so backoff must never starve it.

### No Checks Found (Startup Phase)

**Snapshot mode**: Returns exit code 0, displays message.

**TUI mode**: Shows "Startup Phase" message with phased messaging based on elapsed time:

1. **0-2 minutes**: Helpful "Startup Phase" with spinner
2. **2-3 minutes**: "Still waiting" warning
3. **>3 minutes**: "No checks found" (likely no workflows)

### Premature Exit Prevention (Issue #236)

When fast checks (like DCO) complete before slower checks have appeared in the API response, the TUI prevents premature exit using `canTrustCompletion()`:

1. **Grace period**: After `startupGracePeriod` (2 minutes), completion is always trusted
2. **Appearance ratio**: If `expectedCheckCount` is available from historical averages, the check count must reach `minCheckAppearanceRatio` (30%) of expected
3. **Peak tracking**: If the current check count is less than `peakCheckCount` (meaning checks disappeared), completion is never trusted

The TUI displays a visual "Waiting for more checks to appear..." message during this phase, showing either the appearance ratio or the grace period countdown.

### Copilot Edge Cases

- **Incapable host**: A GHES instance without Copilot code review 404s the reviewer-app lookup; the gate never arms, no probe fetches fire, and neither the row nor the status line renders. Only a definitive 404 disables detection — network/auth/5xx errors keep it enabled so a flaky response can't silently disable a feature the user may rely on
- **No evidence yet**: Until a poll positively sees a Copilot request or review, the synthetic row stays hidden and a neutral "Checking for Copilot review…" line shows instead — most repos don't use Copilot and would otherwise read a phantom queued row as a real review
- **GraphQL read lag**: A just-created review request may not be visible to GraphQL immediately; the two-consecutive-not-requested rule absorbs this (the same lag exists in reverse for the REST POST that creates the request)
- **Stale review**: A Copilot review targeting an old commit doesn't block exit (it's informational and self-corrects); the row shows a "⚠ stale" state with the current short SHA and a "refresh or re-request" hint
- **Give-up**: A requested-but-never-submitted review stops blocking after `copilot_max_wait` (default 3m), measured from the CI cycle's start; the row renders a "⏱ giving up" summary instead of spinning forever (issue #442)

---

## 10. Data Flow Diagrams

### Snapshot Mode Flow (PR)

```text
main.go run()
    │
    ├── config.Load()
    │   └── Returns: Config{RefreshInterval, RepoRefreshInterval, Fade*,
    │                     Colors, EnableLinks, PresumedAverages,
    │                     WaitForCopilot, Copilot* timings}
    │
    ├── tui.NewStyles()
    │   └── Returns: Styles{Success, Failure, Running, Queued, Info, Warning, ...}
    │
    ├── parseArgs() → runArgs{mode, host, owner, repo, prNumber}
    │   ├── PR URL: ghclient.ParsePRURL() → host from URL
    │   ├── Actions URL: ghclient.ParseActionsRunURL() → modeRun (handled by runRunSnapshot)
    │   ├── Number: ghclient.GetPRWithRepo() → host from PR URL
    │   └── Auto: ghclient.GetCurrentPRWithRepo() (GIT_DIR set for jj)
    │       └── Host-less → ghclient.DefaultHost() (GH_HOST or github.com)
    │
    ├── ghclient.GetTokenForHost(host)   ← early fail-fast check
    │
    ├── runPRMode → Check terminal: term.IsTerminal()
    │   └── FALSE: runSnapshot()                        [main.go:378]
    │
    └── runSnapshot()
        │
        ├── ghclient.NewClientFromToken(token, host)
        │   └── Creates REST API client (enterprise URLs derived from host)
        │
        ├── ghclient.FetchPRInfo()                      [internal/github/pr.go]
        │   └── Returns: PRInfo{Title, HeadSHA, CreatedAt}
        │
        ├── ghclient.FetchCheckRunsGraphQL()            [internal/github/graphql.go]
        │   └── Returns: []CheckRunInfo{...}, cycleStart, rateLimit
        │       (cycleStart = max(head push, PR creation), issue #518)
        │
        ├── ghclient.FetchJobAverages() (unless --quick)
        │   └── Returns: map[jobName]weightedAverageDuration
        │
        ├── ghclient.ApplyPresumedAverages()            [internal/github/history.go]
        │   └── Fills external-app checks (DCO) with configured durations
        │
        ├── tui.CalculateColumnWidths()
        │   └── Returns: ColumnWidths{Queue, Name, Duration, Avg}
        │
        ├── Render output
        │   ├── tui.FormatHeaderColumns()
        │   ├── tui.BuildNameColumn()
        │   ├── tui.FormatQueueLatency()
        │   ├── tui.FormatDuration()
        │   ├── tui.FormatAvg()
        │   └── Determine exit code (ghclient.FailureConclusion())
        │
        └── Copilot snapshot (when wait_for_copilot)
            ├── ghclient.CopilotReviewerExistsOnHost()  ← probe first
            │   └── Incapable: print "Copilot: not available on this host"
            └── ghclient.FetchCopilotReview(headSHA)
                └── Print state line; exit 1 on changes_requested
```

The run-mode snapshot path (`runActionsMode → runRunSnapshot`) is parallel but skips the PR metadata fetch and uses `FetchRunInfo` / `FetchRunJobs`, `tui.CalculateRunColumnWidths` / `tui.FormatRunHeaderColumns`, and `ghclient.FailureJobConclusion()`.

### TUI Mode Flow (PR)

```text
main.go runPRMode()
    │
    ├── tui.NewModel(...)                               [internal/tui/model.go]
    │   └── Model{ctx, token, host, owner, repo, prNumber, spinner, scrollState, ...}
    │       - Initializes empty maps: jobAverages, workflowAverages,
    │         advSecMatchWorkflow, runIDToWorkflowID, fetchedWorkflowIDs,
    │         pendingWorkflowFetch, dispatchedWorkflowFetch, seenCheckKeys
    │       - expectedCheckCount = 0, peakCheckCount = 0
    │
    ├── tea.NewProgram(model)
    │
    └── p.Run()                                         [Blocking event loop]
        │
        └── model.Init()                                [internal/tui/update.go]
            │
            ├── Returns: tea.Batch(
            │       spinner.Tick,
            │       fetchPRInfo(),
            │       tick(m.refreshInterval),
            │       fetchCopilotProbe()   [when waitForCopilot]
            │   )
            │
            └── Message processing loop (every Update syncs the viewport)
                │
                ├── [tea.WindowSizeMsg]
                │   └── Store width/height (scroll viewport arms here)
                │
                ├── [PRInfoMsg received]
                │   ├── Store: prTitle, headSHA (stripped of variation selectors)
                │   ├── Arm Copilot gate (unless probe found host incapable):
                │   │     copilotWaitStartTime = now (fallback anchor)
                │   │     copilotPollStartTime = now + copilotInitialDelay
                │   └── Return: fetchCheckRuns()
                │
                ├── [TickMsg received]
                │   ├── Copilot max-wait timeout check (BEFORE backoff —
                │   │   backoff must never starve it, issue #442)
                │   ├── Backoff gate: fetchReceived && rateLimitRemaining < 10?
                │   │   └── YES: tick 3x interval, no fetches
                │   └── Return: tea.Batch(
                │           fetchCheckRuns(),
                │           tick(m.refreshInterval),
                │           fetchCopilotReview()  [when cadence/delay/rate
                │                                 gates all pass]
                │       )
                │
                ├── [ChecksUpdateMsg received]
                │   └── handleChecksUpdate()
                │       ├── SortCheckRuns() by duration
                │       ├── Adopt CycleStartTime anchor on first poll;
                │       │   re-anchor Copilot budget to real cycle start
                │       ├── ApplyPresumedAverages() (DCO etc.)
                │       ├── Track peakCheckCount, firstCheckSeenAt
                │       ├── Mark seenCheckKeys; new checks → re-discovery
                │       ├── readyForHistory (10s delay or allComplete)
                │       │   └── Dispatch discoverWorkflows() if needed
                │       └── allComplete && canTrustCompletion && copilotGateSatisfied?
                │           ├── Set exitCode (checks + copilot state)
                │           └── No pending history fetches → tea.Quit
                │
                ├── [CopilotProbeMsg received]
                │   └── Incapable host: disarm gate, clear evidence,
                │       re-check completion (exit now if checks already done)
                │
                ├── [CopilotReviewMsg received]
                │   └── handleCopilotReview()
                │       ├── Fold rate limit (minimum)
                │       ├── Two consecutive "not requested" → gate clears
                │       ├── Evidence seen → row becomes visible
                │       ├── Pending → keep waiting
                │       └── Complete/stale → gate clears; quit if checks done
                │
                ├── [WorkflowsDiscoveredMsg received]
                │   ├── Merge run→workflow mappings
                │   ├── Dispatch fetchWorkflowHistory() per new workflow
                │   ├── AdvSec name matches → alias averages, dispatch fetches
                │   └── No new fetches → discovery phase complete
                │
                ├── [JobAveragesPartialMsg received]
                │   ├── Merge averages; cache per workflow (workflowAverages)
                │   ├── Write AdvSec aliases into jobAverages
                │   ├── Update: expectedCheckCount = len(jobAverages)
                │   └── Last pending fetch → discovery complete; quit if checks done
                │
                ├── [spinner.TickMsg received]
                │   └── Update spinner animation (View re-renders)
                │
                └── [tea.KeyMsg received]
                    ├── "q" or "ctrl+c": tea.Quit
                    └── otherwise: scrollState.handleKey()
                        (j/k lines, ^f/^b pages, ^d/^u half, gg/G, home/end)
```

On exit, `printFinalFrame` re-renders the final model via `FullView()` so the completed summary lands in scrollback despite alt-screen restore (issues #451, #554).

### Forked PR Detection Flow

```text
GetPRWithRepo() or GetCurrentPRWithRepo()
    │
    ├── Exec: gh pr view --json number,url (GIT_DIR set for jj via SetGITDirForJJ)
    │
    ├── Parse JSON: {number, url}
    │
    └── ParsePRURL(url)
        └── Extract host, owner, repo from URL
            └── Returns: host, owner, repo, prNumber
                (from the upstream repo the PR lives in — not the local fork
                remote, and with the enterprise host when applicable)
```

### Repo Mode Flow

```text
main.go run() with --repo
    │
    ├── Validate: --repo not combined with positionals / --quick / non-tty
    │
    ├── config.Load()
    ├── tui.NewStyles()
    │
    ├── resolveRepoArg(repoFlag)                        [main.go:217-226]
    │   ├── Explicit: ghclient.ParseRepoArg()
    │   │     (owner/repo, host/owner/repo, or URL; rejects _ segments)
    │   └── Bare: ghclient.GetCurrentRepo() (git remote get-url origin)
    │   └── Empty host → ghclient.DefaultHost()
    │
    ├── ghclient.GetTokenForHost(host)  ← fail fast on hosts without credentials
    │
    └── runRepoMode()                                   [main.go:324-357]
        │
        ├── tui.NewRepoModel(refresh=cfg.RepoRefreshInterval,
        │                  fadeSuccess, fadeFailure)
        ├── tea.NewProgram(model)
        └── p.Run()
            │
            └── model.Init()                            [internal/tui/repoupdate.go]
                │
                ├── Returns: tea.Batch(
                │       spinner.Tick,
                │       fetchRepoCheckRuns(),           [GraphQL, no annotations]
                │       fetchRepoRuns(fadeWindow),      [REST, ExcludePullRequests]
                │       repoTick(refreshInterval)
                │   )
                │
                └── Message processing loop (Update syncs the viewport)
                    │
                    ├── [RepoTickMsg]
                    │   ├── Backoff if fetchReceived && rate < 10 (3x interval)
                    │   └── Re-dispatch both fetches + next tick
                    │
                    ├── [RepoChecksUpdateMsg]
                    │   ├── On err: record fetchErrChecks, keep last good m.prs
                    │   ├── Else: fade-filter checks per PR → m.prs
                    │   │        (preserve ExtraCheckRuns from last runs poll)
                    │   └── rateLimitRemaining = min/first across sources
                    │
                    ├── [RepoRunsUpdateMsg]
                    │   ├── On err: record fetchErrRuns, keep last good runs
                    │   ├── Else: fade-filter runs
                    │   └── dedupeAndAttachExtraJobs() reconciles vs PRs
                    │        (drop PR-owned jobs, attach extras under PRs)
                    │
                    └── [tea.KeyMsg q/ctrl+c] → tea.Quit (exitCode stays 0)
```

---

## 11. Exit Behavior

### Exit Codes

| Code | Meaning | Example |
| ---- | ------- | ------- |
| 0 | Success | All checks passed |
| 0 | No checks | PR has no workflows (snapshot mode) |
| 0 | Incomplete checks | Checks still running (snapshot mode) |
| 0 | User quit | Repo mode always returns 0 (only exits on q/ctrl+c) |
| 1 | Check failure | One or more checks failed |
| 1 | Copilot changes requested | Review state `changes_requested` (PR mode, `wait_for_copilot`) |
| 1 | Authentication error | Missing token for the host |
| 1 | Network error | Failed to fetch PR info, check runs, run info, or jobs (snapshot mode) |
| 1 | Invalid input | Bad PR number, URL, or repo argument; `--repo` with positionals / `--quick` / non-tty |

### Exit Code Determination

```go
func determineExitCode(checks []ghclient.CheckRunInfo, copilotState string, waitForCopilot bool) int {
    for _, check := range checks {
        if ghclient.FailureConclusion(check.Conclusion) {
            return 1
        }
    }
    if waitForCopilot && ghclient.CopilotReviewFails(copilotState) {
        return 1
    }
    return 0
}
```

**Failure Conditions** (from `conclusion.go`):

- `failure`: Test failures, build errors
- `timed_out`: GitHub Actions timeout
- `action_required`: Waiting for manual approval

**Success Conditions**:

- `success`: All steps passed
- `cancelled`: User manually cancelled
- `skipped`: Job skipped due to conditions
- `neutral`: Check completed with neutral status

### Completion Check

```go
func allChecksComplete(checks []ghclient.CheckRunInfo) bool {
    if len(checks) == 0 {
        return false  // Keep polling if no checks yet
    }

    for _, check := range checks {
        if check.Status != "completed" {
            return false
        }
    }

    return true
}
```

**Critical Edge Case**: Empty check list returns `false`, preventing premature exit during startup phase.

### Premature Exit Prevention

Even when `allChecksComplete()` returns `true`, the TUI applies an additional `canTrustCompletion()` gate (see [Completion Gate](#completion-gate-cantrustcompletion-updatego18-61)) to prevent exiting before all expected checks have appeared, plus the `copilotGateSatisfied()` gate to prevent exiting while a Copilot review is still pending. When completion can't be trusted, the TUI displays "Waiting for more checks to appear..." with either an appearance ratio or grace period countdown.

### Clean Shutdown

TUI mode exits cleanly by:

1. Setting `m.quitting = true`
2. Returning `tea.Quit` command
3. Bubbletea restores terminal state (alt-screen mode restores the pre-launch screen content)
4. `printFinalFrame` re-renders the final model to stdout — `FullView()` for scrolling models so the complete, unclipped summary lands in scrollback — so the completed run's output survives after the alt screen releases
5. Exit code extracted from the final model
6. `os.Exit(exitCode)` terminates process

Repo mode is the exception: it's persistent and only quits on `q`/`ctrl+c`, so `ExitCode()` always returns 0.

---

## 12. Repo Mode

Repo mode (`--repo`, `main.go:324-357` via `runRepoMode()`) is a persistent dashboard for a whole repository: it shows **all active PRs' checks** alongside **standalone (non-PR) branch runs**, fading completed checks out so the view stays focused on what's in flight.

### Why it's separate from PR/Run modes

- **Different lifecycle**: persistent, never auto-exits, no exit-code semantics
- **Two data sources**: a batched GraphQL query for all open PRs plus a REST query for standalone workflow runs — these run independently and their errors are tracked separately so a success from one source can't mask an ongoing error from the other
- **Interactive only**: snapshot mode is rejected in `run()` (an error is printed and `1` returned), since the dashboard only makes sense live

### Arg resolution (`main.go:217-226` via `resolveRepoArg()`)

```go
func resolveRepoArg(val string) (string, string, string, error) {
    if val != "" && val != repoFlagAutoSentinel {
        return ghclient.ParseRepoArg(val)
    }
    host, owner, repo, err := ghclient.GetCurrentRepo()
    if err != nil {
        return "", "", "", fmt.Errorf("failed to detect current repo: %v\nUse --repo owner/repo to specify explicitly", err)
    }
    return host, owner, repo, nil
}
```

- Explicit value → `ghclient.ParseRepoArg()` accepts `owner/repo`, `host/owner/repo` (the host segment must contain a dot, so a 3-segment owner/repo typo like `org/team/repo` isn't swallowed), or `https://github.com/owner/repo`-style URLs. PR/Actions-run URLs are rejected; all-underscore segments are rejected so `_` stays a valid sentinel
- Bare `--repo` → `ghclient.GetCurrentRepo()` reads `git remote get-url origin` and parses SSH or HTTPS remote URLs, capturing the host (an enterprise remote resolves against the enterprise instance)
- A host-less value → the caller fills it from `DefaultHost()` (`GH_HOST` or github.com), and `GetTokenForHost` fails fast before the TUI starts

### Model (`internal/tui/repomodel.go`)

`RepoModel` holds:

- `prs map[int]PRViewData` — already fade-filtered PR check data keyed by PR number. `PRViewData` carries the fade-filtered `CheckRuns` (GraphQL-sourced), `ExtraCheckRuns` (REST-sourced extras like Copilot, attached by the dedup pass), the cycle-start anchor, and `HeadSHA` for run reconciliation
- `standaloneRuns []ghclient.BranchRunData` — already fade-filtered standalone runs
- `fadeSuccess`, `fadeFailure` — fade-out windows (defaults 15m / 30m from config)
- `fetchErrChecks` / `fetchErrRuns` + timestamps — split per-source error tracking so transient 504s from one source don't wipe the other's error state
- `fetchReceived` — gates the rate-limit indicator and backoff so the zero-value `0` doesn't render misleadingly before the first response
- embedded `scrollState` — the dashboard scrolls when it's taller than the terminal (issue #542)

`ExitCode()` always returns 0; repo mode is persistent.

### Init and poll loop (`internal/tui/repoupdate.go`)

```go
func (m RepoModel) Init() tea.Cmd {
    return tea.Batch(
        m.spinner.Tick,
        fetchRepoCheckRuns(m.ctx, m.token, m.host, m.owner, m.repo),
        fetchRepoRuns(m.ctx, m.token, m.host, m.owner, m.repo, m.fadeWindow()),
        repoTick(m.refreshInterval),
    )
}
```

Each tick batches **both** the GraphQL PR checks fetch and the REST standalone-runs fetch. Rate-limit backoff is gated on `m.fetchReceived` (mirroring PR/Run models) so the zero-value `rateLimitRemaining` before the first response doesn't pin the model in backoff forever.

**Messages**:

- `RepoTickMsg` — poll timer; re-dispatches both fetches
- `RepoChecksUpdateMsg` — `{PRData map[int]PRCheckData, RateLimitRemaining int, Err error}`
- `RepoRunsUpdateMsg` — `{Runs []BranchRunData, RateLimitRemaining int, Err error}`
- `spinner.TickMsg`, `tea.KeyMsg` (q/ctrl+c quit; other keys scroll)

### Fade-out filtering (`handleRepoChecksUpdate`, `handleRepoRunsUpdate`)

Both handlers apply the same fade logic:

- **Active** statuses (`in_progress`, `queued`, `waiting`, and for runs also `pending`) are always kept
- **Completed** checks are kept if `now - CompletedAt < fadeTimeout`, where `fadeTimeout = fadeSuccess` for success and `fadeFailure` for failures (`FailureConclusion`)
- Otherwise the check/run drops off the screen

Transient fetch errors are **non-fatal**: the last good `m.prs` / `m.standaloneRuns` is preserved, the error is recorded with a timestamp against its own source, and polling continues. The view renders whichever source's error is most recent (prefixed by "PR checks" or "Repo runs").

### PR/run deduplication (`dedupeAndAttachExtraJobs`, issue #331)

The two data sources overlap: a PR's checks also appear as branch runs on the same commit. After fade-out, `dedupeAndAttachExtraJobs` reconciles the standalone runs against the PR section:

- Jobs are identified by a dedup key of lowercase `headSHA \0 workflow \0 name` (SHA-scoped so jobs never dedup across commits; NUL separators so names containing separators can't collide; lowercased to absorb case variants across the GraphQL and REST paths)
- A run whose jobs all appear in a tracked PR's authoritative `CheckRuns` is dropped from the standalone section; leftover "extra" jobs the PR GraphQL query misses (e.g. Copilot-triggered runs) are attached under the matching PR's `ExtraCheckRuns` and render inside the PR group
- Runs matching no tracked PR stay standalone; completed standalone runs with no leftover jobs are dropped entirely
- `ExtraCheckRuns` is **rebuilt** every runs tick (not appended) so extras whose run has faded out don't linger under the PR, but is preserved across PR-checks polls so extras don't flicker out between runs ticks
- The two fetch handlers run concurrently in arbitrary order; if PR data hasn't arrived yet, every run shows standalone and reconciles on the next tick

### Rate-limit handling across sources

Both handlers take the minimum across the two sources, but accept the first observed value so the zero default doesn't pin `rateLimitRemaining` at 0 forever:

```go
if !m.fetchReceived || msg.RateLimitRemaining < m.rateLimitRemaining {
    m.rateLimitRemaining = msg.RateLimitRemaining
}
```

Absent rate limits (enterprise hosts with rate limiting disabled) are normalized at the source layer, so a 0 reaching here is a real exhausted quota and is honored.

### Rendering (`internal/tui/repoview.go`)

`RepoModel.View()` composes pinned header, the scrolling body viewport, and pinned footer. The body renders:

1. A repo header (`owner/repo` + UTC clock) and summary line (`N active PRs • M branch runs • Updated Xs ago` — branch runs only shown when nonzero)
2. Per-PR groups (`PR #NN: Title`) followed by their checks, merging `CheckRuns` and `ExtraCheckRuns` and reusing `display.go` helpers with a nil `jobAverages` (no HistAvg column in repo mode)
3. A standalone-runs section: runs grouped by branch (`Branch: name`), each run header showing icon + title + event annotation + duration, followed by its jobs in a two-column layout
4. A two-tier rate-limit indicator (red under `minRateLimitForFetch`, yellow under `rateWarningThreshold`), only rendered after the first response
5. An optional non-fatal fetch-error status line (`[PR checks fetch error: ... — 12s ago]`, truncated via `truncateFetchError` so embedded HTML from 504s doesn't span many lines)
6. A scroll hint (when the body overflows) or `Press q to quit`

`SortCheckRuns` is reused for PR checks; standalone runs are sorted by branch name via `sortedBranchNames()` and PR groups by `sortedPRNumbers()`.

### GitHub API layer for repo mode

- `internal/github/repo.go` — `ParseRepoArg`, `GetCurrentRepo` (SSH/HTTPS remote parsing with all-underscore-segment rejection and host capture)
- `internal/github/repo_graphql.go` — `FetchRepoCheckRunsGraphQL` fetches up to `maxPRsPerQuery` (10) most-recently-updated open PRs with their check rollups in a single query. Uses a trimmed `repoContextNode` that **omits** `annotations(first: 5)` — annotations are the most expensive field and push the query over GitHub's GraphQL cost limit on high-traffic repos. Repo mode never renders annotation boxes, so dropping them is safe and makes the query 10/10 reliable. Each PR gets its own cycle-start anchor (`max(push, creation)`, issue #518) for the queue-latency column, and its head commit OID for the dedup pass
- `internal/github/repo_runs.go` — `FetchRepoWorkflowRuns` lists standalone runs with `ExcludePullRequests: true`, issuing two REST calls (in_progress, then recently-created within `fadeWindow`) and deduplicating. Uses RFC3339 timestamps (not date-only) so a 30m window queries the last 30 minutes, not the whole calendar day — the date-only form triggered 504s on busy repos. `fetchRepoRunPage` deliberately does **not** follow pagination for the same 504-avoidance reason: the first 100 runs is enough for the persistent view. `EnrichRepoRunsWithJobs` then fetches per-run jobs; job-enrichment failure is non-fatal (runs come back with empty `Jobs` so headers still render)
- `PRCheckData` and `BranchRunData` are the result types; `WorkflowJobInfo` and `CheckRunInfo` from PR/Run modes are reused where possible

### Configuration additions

`internal/config/config.go` adds three repo-mode defaults:

- `repo_refresh_interval: 30s` — repo mode poll interval (separate from PR/run modes' 5s, since repo mode makes more API calls per poll)
- `fade_success: 15m` — how long a passing completed check stays on screen
- `fade_failure: 30m` — how long a failed completed check stays on screen (longer so failures don't vanish before a human notices)

---

## Summary

gh-observer demonstrates several best practices:

1. **Clean separation of concerns**: Distinct packages for config, GitHub API, timing, and TUI rendering
2. **Efficient API usage**: GraphQL for complex queries, REST for simple metadata; repo mode uses a cost-trimmed GraphQL query (no annotations) to stay under GitHub's limits
3. **Graceful error handling**: Non-fatal errors during polling, fatal errors at initialization; repo mode tracks per-source errors so a success from one source can't mask an ongoing error from another
4. **Terminal-aware output**: Snapshot mode for CI, TUI mode for interactive use; repo mode is interactive-only
5. **Rate limit awareness**: Backoff strategy, two-tier warning indicator, `fetchReceived` gating so the zero-value rate limit doesn't mislead before the first response, and GHES-aware normalization so absent rate-limit data isn't read as an exhausted quota
6. **Streaming data fetching**: Historical averages fetched per-workflow to reduce latency and provide early feedback, with re-discovery when new jobs appear
7. **User feedback**: Startup phase messaging, real-time updates, fetch progress display, repo-mode summary line
8. **Fork support**: Correctly identifies the upstream repository (and host) for forked PRs
9. **Delayed fetching**: Waits 10 seconds after first checks appear before fetching historical averages
10. **Concurrent coordination**: Uses pending/dispatched tracking to coordinate multiple async fetches
11. **Premature exit prevention**: Uses `canTrustCompletion()` with grace period, appearance ratio, peak tracking, and a quick-mode shortcut to prevent exiting when fast checks complete before others appear
12. **GHAS and third-party app detection**: Uses `AppName` from `checkSuite.app` for meaningful names, aliases Actions history onto name-matched AdvSec checks, and fills external-app checks (DCO) with configured presumed averages
13. **Three input modes**: PR checks, standalone Actions runs, and persistent repo watching — each with its own model/update/view files but sharing display helpers, constants, the scroll subsystem, and the history-fetch pipeline
14. **Stable duration formatting**: `FormatDuration` always renders smaller units down to seconds to avoid display flicker (issue #314)
15. **GitHub Enterprise support**: Every argument form carries a host, endpoints mirror go-gh's derivation (ghe.com subdomains, GHES path endpoints, env-var overrides), enterprise tokens resolve like the gh CLI, and rate-limit handling survives hosts with rate limiting disabled
16. **Copilot code review tracking**: A parallel GraphQL pipeline detects Copilot reviews that StatusCheckRollup can't see, gates exit on review completion, bounds its own wait with a give-up timeout, and renders a synthetic table row that stays out of all completion/timing logic
17. **Scrolling and alt-screen hygiene**: All interactive modes pin header/footer and scroll the body vim-style; alt-screen mode prevents stale-frame corruption and a final-frame reprint lands the completed summary in scrollback

The codebase follows the Elm Architecture pattern through Bubbletea, making the state management predictable and testable. The linear execution flow from initialization through polling to exit is clear and well-structured.

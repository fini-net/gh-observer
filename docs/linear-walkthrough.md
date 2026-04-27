# Linear Code Walkthrough

This document provides a comprehensive walkthrough of the gh-observer codebase, following the execution flow from entry point through all code paths. It's designed for contributors who need a deep technical understanding of how the application works.

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

---

## 1. Application Entry Point

### Command Registration (`main.go:26-48`)

The application uses [Cobra](https://github.com/spf13/cobra) for CLI argument parsing. The root command is registered with:

- **Usage**: `gh-observer [PR_NUMBER | PR_URL]`
- **Arguments**: Maximum of 1 argument (optional PR number or full PR URL)
- **Flags**:
  - `--quick` / `-q`: Skip fetching historical average runtimes
  - `--debug` / `-d`: Enable structured debug logging to `os.TempDir()/gh-observer-debug/`
- **Execution**: Calls `run(args)` and exits with the returned exit code

```go
var quickFlag bool
var debugFlag bool

func init() {
    rootCmd.Flags().BoolVarP(&quickFlag, "quick", "q", false, "Skip fetching historical average runtimes")
    rootCmd.Flags().BoolVarP(&debugFlag, "debug", "d", false, "Log suppressed errors and internal state to a file")
}

var rootCmd = &cobra.Command{
    Use:   "gh-observer [PR_NUMBER | PR_URL]",
    Short: "Watch GitHub PR checks with runtime metrics",
    Long: `gh-observer is a GitHub PR check watcher CLI tool that improves on 
'gh pr checks --watch' by showing runtime metrics, queue latency, 
and better handling of startup delays.

Supports watching checks on external repositories by passing a full PR URL:
  gh-observer https://github.com/owner/repo/pull/123`,
    Args: cobra.MaximumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        exitCode := run(args)
        os.Exit(exitCode)
    },
}
```

**Design Decision**: The exit code is captured and passed to `os.Exit()` explicitly. This allows the TUI to clean up properly before exiting.

### URL Support (`main.go:168-191`)

The application accepts either a PR number or a full GitHub PR URL:

```go
if len(args) > 0 {
    arg := args[0]
    if owner, repo, prNumber, err = ghclient.ParsePRURL(arg); err == nil {
        // valid PR URL
    } else if n, convErr := strconv.Atoi(arg); convErr == nil {
        // numeric PR number
        prNumber, owner, repo, err = ghclient.GetPRWithRepo(n)
    } else {
        fmt.Fprintf(os.Stderr, "Invalid PR number or URL: %s\n", arg)
        return 1
    }
} else {
    // Auto-detect from current branch (correctly handles forks)
    prNumber, owner, repo, err = ghclient.GetCurrentPRWithRepo()
}
```

**Why use PR URL?** External repositories can be watched without cloning them locally. The URL contains all the information needed (owner, repo, PR number).

**Fork Handling**: The code uses `GetPRWithRepo()` and `GetCurrentPRWithRepo()` instead of `ParseOwnerRepo()` to correctly identify the repository for forked PRs. The local git remote might point to a fork, but the PR lives in the upstream repository.

### Main Run Function (`main.go:137-223`)

The `run()` function orchestrates all initialization and mode selection:

#### Step 1: Debug Logging Setup (`main.go:140-147`)

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

When `--debug` is enabled, structured debug logging via `slog` writes to `os.TempDir()/gh-observer-debug/`. Debug statements throughout the codebase log key events like check updates, rate limit backoff, and completion trust decisions.

#### Step 2: Configuration Loading (`main.go:149-154`)

```go
cfg, err := config.Load()
```

Calls `internal/config/config.go` which:

1. Creates a new Viper instance
2. Sets defaults:
   - `refresh_interval: 5s`
   - `colors.success: 10` (green)
   - `colors.failure: 9` (red)
   - `colors.running: 11` (yellow)
   - `colors.queued: 8` (gray)
   - `enable_links: true`
3. Reads config from `~/.config/gh-observer/config.yaml` (if exists)
4. Falls back to defaults if config file missing
5. Unmarshals into `Config` struct

#### Step 3: Style Creation (`main.go:157-162`)

```go
styles := tui.NewStyles(
    cfg.Colors.Success,
    cfg.Colors.Failure,
    cfg.Colors.Running,
    cfg.Colors.Queued,
)
```

Creates Lipgloss styles for rendering colored output. See `internal/tui/styles.go` for implementation.

#### Step 4: PR Resolution (`main.go:164-191`)

Two scenarios:

**Explicit PR number**: Uses `GetPRWithRepo(n)` to get owner/repo from the PR URL (handles forks correctly).

**Explicit PR URL**: Uses `ParsePRURL(url)` to extract owner/repo/number directly — this uses the same approach as `main.go:170` where `ParsePRURL` is tried first:

```go
if owner, repo, prNumber, err = ghclient.ParsePRURL(arg); err == nil {
    // valid PR URL
} else if n, convErr := strconv.Atoi(arg); convErr == nil {
    // numeric PR number
    prNumber, owner, repo, err = ghclient.GetPRWithRepo(n)
}
```

**Auto-detection**: Uses `GetCurrentPRWithRepo()` which calls `gh pr view --json number,url` and extracts the correct repository from the PR URL.

#### Step 5: Authentication (`main.go:193-198`)

```go
token, err := ghclient.GetToken()
```

Located at `internal/github/client.go`. Token acquisition strategy:

1. **First**: Check `GITHUB_TOKEN` environment variable
2. **Fallback**: Run `gh auth token` command
3. **Error**: Return message if both fail

#### Step 6: Mode Selection (`main.go:200-204`)

```go
if !term.IsTerminal(int(os.Stdout.Fd())) {
    return runSnapshot(ctx, token, owner, repo, prNumber, cfg.EnableLinks, quickFlag)
}
```

Uses `golang.org/x/term` to detect if stdout is a terminal:

- **Not a terminal** (piped, redirected, or CI): Runs snapshot mode
- **Is a terminal**: Runs interactive TUI mode

---

## 2. GitHub Authentication & Setup

### REST API Client Creation (`internal/github/client.go`)

Both snapshot mode and PR info fetching use REST API:

```go
func NewClient(ctx context.Context) (*github.Client, error) {
    token, err := GetToken()
    if err != nil {
        return nil, err
    }
    
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
    tc := oauth2.NewClient(ctx, ts)
    return github.NewClient(tc), nil
}
```

Uses `google/go-github/v85` library with OAuth2 token authentication.

### GraphQL Client Creation (`internal/github/graphql.go`)

Check run fetching uses GraphQL for efficiency:

```go
src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
httpClient := oauth2.NewClient(ctx, src)
client := githubv4.NewClient(httpClient)
```

**Design Decision: Why use both REST and GraphQL?**

- **REST for PR metadata**: PR info (title, SHA, timestamps) is simple and REST API is straightforward
- **GraphQL for check runs**: Single query fetches workflow name + job name + status + timestamps. Equivalent REST calls would require multiple API calls.

---

## 3. Execution Path A: Snapshot Mode

Snapshot mode runs when stdout is not a terminal (e.g., scripts, CI, redirected output).

### Implementation (`main.go:50-135`)

#### Step 1: Fetch PR Metadata

```go
client, err := ghclient.NewClient(ctx)
prInfo, err := ghclient.FetchPRInfo(ctx, client, owner, repo, prNumber)
headCommitTime, err := time.Parse(time.RFC3339, prInfo.HeadCommitDate)
```

Uses REST API to get PR title, head SHA, and timestamps.

#### Step 2: Fetch Check Runs

```go
checkRuns, _, err := ghclient.FetchCheckRunsGraphQL(ctx, token, owner, repo, prNumber)
```

Returns `[]CheckRunInfo` with workflow names, status, timestamps, and annotations.

#### Step 3: Handle Empty Checks

```go
if len(checkRuns) == 0 {
    sinceCreation := time.Since(headCommitTime)
    fmt.Printf("No checks found (commit pushed %s ago)\n", timing.FormatDuration(sinceCreation))
    return 0
}
```

#### Step 4: Fetch Historical Averages (unless `--quick`)

```go
var jobAverages map[string]time.Duration
if !quick {
    client, err := ghclient.NewClient(ctx)
    if err == nil {
        avgs, _, _, err := ghclient.FetchJobAverages(ctx, client, owner, repo, checkRuns, nil, nil)
        if err == nil {
            jobAverages = avgs
        }
    }
}
```

Located at `internal/github/history.go:31-153`. Fetches recent completed workflow runs to calculate average job durations.

#### Step 5: Calculate Column Widths

```go
widths := tui.CalculateColumnWidths(checkRuns, headCommitTime, jobAverages)
```

Now includes a 4th column for historical averages.

#### Step 7: Render Output

```go
headerQueue, headerName, headerDuration, headerAvg := tui.FormatHeaderColumns(widths)
fmt.Printf("%s   %s  %s  %s\n\n", headerQueue, headerName, headerDuration, headerAvg)
```

**Output Format**:

```text
Start     Workflow/Job        ThisRun  HistAvg

42s    ✓  Build / test          2m 15s    2m 0s
1m 5s  ◐  Lint / check            1m 3s    45s
```

#### Step 8: Exit Code Determination

Uses `ghclient.FailureConclusion()` from `internal/github/conclusion.go`:

```go
if check.Status == "completed" {
    if ghclient.FailureConclusion(check.Conclusion) {
        exitCode = 1
    }
}
```

---

## 4. Execution Path B: Interactive TUI Mode

TUI mode runs when stdout is a terminal, providing real-time updates.

### Model Creation (`main.go:207`)

```go
model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag)
```

The `type Model struct` definition (`internal/tui/model.go:12-67`) holds all TUI state:

```go
type Model struct {
    // Context and GitHub data
    ctx      context.Context
    token    string
    owner    string
    repo     string
    prNumber int
    
    // PR metadata (populated later)
    prTitle        string
    headSHA        string
    prCreatedAt    time.Time
    headCommitTime time.Time
    
    // Check run data (updated every poll)
    checkRuns []ghclient.CheckRunInfo
    rateLimitRemaining int
    
    // Historical job averages (incrementally updated)
    jobAverages             map[string]time.Duration
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
    
    // Set when all checks complete; used to defer quit until avgFetchDone
    checksComplete bool

    // Premature exit prevention (issue #236)
    expectedCheckCount int
    peakCheckCount     int
    
    // UI state
    spinner         spinner.Model
    startTime       time.Time
    lastUpdate      time.Time
    refreshInterval time.Duration
    styles          Styles
    
    // Exit tracking
    exitCode int
    quitting bool
    
    // Error state
    err error
    
    // Feature flags
    enableLinks bool
}
```

The `NewModel(...)` constructor (`internal/tui/model.go:70-92`) initializes the Bubbletea model with defaults.

**Premature Exit Prevention**: `expectedCheckCount` tracks how many distinct job names the history fetch has discovered (set from `len(m.jobAverages)` each time a partial result arrives). `peakCheckCount` tracks the maximum number of checks seen in any single poll. These fields power the `canTrustCompletion()` function that prevents exiting when fast checks (like DCO) complete before slower checks have even appeared in the API response.

### Program Initialization (`main.go:210-211`)

```go
p := tea.NewProgram(model)
finalModel, err := p.Run()
```

Creates a Bubbletea program and enters the event loop.

---

## 5. TUI Message Processing Loop

Bubbletea follows the Elm Architecture pattern: **Model → Update → View** loop.

### Initialization (`internal/tui/update.go:14-19`)

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(
        m.spinner.Tick,
        fetchPRInfo(m.ctx, m.token, m.owner, m.repo, m.prNumber),
        tick(m.refreshInterval),
    )
}
```

Returns three commands that run concurrently:

1. **Spinner tick**: Animates the loading indicator
2. **PR info fetch**: Gets PR title, SHA, timestamps (REST API call)
3. **Tick timer**: Schedules periodic polling

### Message Types (`internal/tui/messages.go`)

```go
type TickMsg time.Time              // Poll timer fired

type PRInfoMsg struct {             // PR metadata received
    Number         int
    Title          string
    HeadSHA        string
    CreatedAt      time.Time
    HeadCommitTime time.Time
    Err            error
}

type ChecksUpdateMsg struct {        // Check runs updated
    CheckRuns          []ghclient.CheckRunInfo
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

type ErrorMsg struct {               // Error occurred
    Err error
}
```

### Update Function (`internal/tui/update.go`)

The `Update()` method handles all incoming messages:

#### Premature Exit Prevention (`update.go:14-53`)

The `canTrustCompletion()` function prevents premature exit when fast checks (like DCO) complete before other jobs have appeared in the API response (issue #236):

```go
func canTrustCompletion(m *Model) bool {
    if m.firstCheckSeenAt.IsZero() {
        return false
    }

    checkCount := len(m.checkRuns)
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

**Three-tier trust logic**:

1. **Grace period elapsed** (`startupGracePeriod` = 2 minutes): Always trust completion
2. **Appearance ratio met** (`minCheckAppearanceRatio` = 30%): Trust if we've seen enough of the expected checks
3. **Checks disappearing** (current count < peak): Never trust — some checks vanished from the API

The `expectedCheckCount` is derived from `len(m.jobAverages)` after each `JobAveragesPartialMsg`, since each job name in the historical averages represents a check that should eventually appear.

#### Message: Check Runs Updated (`update.go`)

Now includes premature exit prevention logic:

```go
if len(msg.CheckRuns) > m.peakCheckCount {
    m.peakCheckCount = len(msg.CheckRuns)
}
```

And the completion check gates on `canTrustCompletion()`:

```go
if allChecksComplete(m.checkRuns) && canTrustCompletion(m) {
    m.exitCode = determineExitCode(m.checkRuns)
    m.checksComplete = true
    // Only quit if no pending/dispatched workflow fetches
    if !m.avgFetchPending && len(m.pendingWorkflowFetch) == 0 {
        m.quitting = true
        cmds = append(cmds, tea.Quit)
    }
    return m, tea.Batch(cmds...)
}
```

#### Message: Partial Averages Received (updated)

The handler now updates `expectedCheckCount` from the historical averages:

```go
case JobAveragesPartialMsg:
    delete(m.pendingWorkflowFetch, msg.WorkflowID)
    m.fetchedWorkflowIDs[msg.WorkflowID] = true

    if msg.Err == nil && msg.Averages != nil {
        maps.Copy(m.jobAverages, msg.Averages)
        m.expectedCheckCount = len(m.jobAverages)
    }
    // ...
```

### handleChecksUpdate (`internal/tui/update.go`)

The check update logic includes streaming historical average fetching and premature exit prevention:

```go
func (m *Model) handleChecksUpdate(msg ChecksUpdateMsg) (tea.Model, tea.Cmd) {
    if msg.Err != nil {
        m.err = msg.Err
        return m, nil  // Continue polling on network errors
    }

    m.checkRuns = msg.CheckRuns
    SortCheckRuns(m.checkRuns)  // Sort by duration
    m.rateLimitRemaining = msg.RateLimitRemaining
    m.lastUpdate = time.Now()
    m.err = nil

    // Track peak check count for premature exit prevention
    if len(msg.CheckRuns) > m.peakCheckCount {
        m.peakCheckCount = len(msg.CheckRuns)
    }

    var cmds []tea.Cmd

    // Track first check seen time for delayed history fetch
    if m.firstCheckSeenAt.IsZero() && len(msg.CheckRuns) > 0 {
        m.firstCheckSeenAt = time.Now()
    }

    // Fetch historical averages after delay or when checks complete
    allComplete := allChecksComplete(msg.CheckRuns)
    elapsed := time.Since(m.firstCheckSeenAt)
    readyForHistory := !m.noAvg && !m.firstCheckSeenAt.IsZero() && (allComplete || elapsed >= historyFetchDelay)
    if readyForHistory && !m.avgFetchPending && m.rateLimitRemaining >= minRateLimitForFetch {
        // Discover workflows and dispatch individual fetches
        cmd := discoverWorkflows(m.ctx, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs)
        cmds = append(cmds, cmd)
    }

    if allChecksComplete(m.checkRuns) && canTrustCompletion(m) {
        m.exitCode = determineExitCode(m.checkRuns)
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

**Key Changes**:

1. **Delayed History Fetch**: Waits 10 seconds after first check appears before fetching history (via `historyFetchDelay` constant)
2. **Streaming Discovery**: Uses `discoverWorkflows()` to find workflow IDs, then dispatches individual `fetchWorkflowHistory()` calls
3. **Pending Tracking**: Tracks `pendingWorkflowFetch` and `dispatchedWorkflowFetch` maps to coordinate concurrent fetches
4. **Exit Coordination**: Waits for all workflow fetches to complete before quitting
5. **Premature Exit Prevention**: `canTrustCompletion()` gates the exit decision, preventing exit when checks appear complete but more are expected

### Check Sorting (`internal/tui/display.go:250-268`)

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

**Sorting Priority**: duration (shortest first) → status (in_progress first) → name (alphabetical)

### Exit Code Determination (`internal/tui/update.go`)

```go
func determineExitCode(checks []ghclient.CheckRunInfo) int {
    for _, check := range checks {
        if ghclient.FailureConclusion(check.Conclusion) {
            return 1
        }
    }
    return 0
}
```

Uses `FailureConclusion()` from `internal/github/conclusion.go` to check for failure states.

### Completion Gate: `canTrustCompletion()`

Before exiting, the TUI verifies that all checks have truly completed using `canTrustCompletion()` (`internal/tui/update.go:14-53`):

```go
if allChecksComplete(m.checkRuns) && canTrustCompletion(m) {
    // Safe to exit
}
```

This prevents premature exit when fast checks (e.g., DCO) complete before slow checks have appeared in the API response. The function uses three tiers:

1. **Grace period** (`startupGracePeriod` = 2 minutes): After this time, completion is always trusted
2. **Appearance ratio** (`minCheckAppearanceRatio` = 30%): If `expectedCheckCount` is known from history, trust when `currentCount / expectedCount >= 0.3`
3. **Peak tracking**: If `currentCount < peakCheckCount`, checks have disappeared and completion cannot be trusted

---

## 6. GitHub API Layer Deep Dive

### GraphQL Query Structure (`internal/github/graphql.go`)

The GraphQL query mirrors the structure used by `gh pr checks`:

```graphql
query($owner: String!, $repo: String!, $prNumber: Int!) {
    repository(owner: $owner, name: $repo) {
        pullRequest(number: $prNumber) {
            commits(last: 1) {
                nodes {
                    commit {
                        statusCheckRollup {
                            contexts(first: 100) {
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
                                                workflow { name }
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
                            }
                        }
                    }
                }
            }
        }
    }
    rateLimit { remaining }
}
```

**App Name Detection**: The `checkSuite.app` field was added to detect GitHub Advanced Security (GHAS) checks and third-party apps (like Checkov) that don't have a `workflowRun`. The `AppName` field in `CheckRunInfo` stores this value, and the display layer uses it as a fallback prefix when `WorkflowName` is empty — so "analyze" from GitHub Code Scanning renders as "GitHub Code Scanning / analyze" instead of just "analyze".

### CheckRunInfo Structure (`internal/github/graphql.go`)

```go
type CheckRunInfo struct {
    Name         string
    WorkflowName string    // From checkSuite.workflowRun.workflow.name
    AppName      string    // From checkSuite.app.name (GHAS, third-party apps)
    Summary      string
    Status       string
    Conclusion   string
    StartedAt    *time.Time
    CompletedAt  *time.Time
    DetailsURL   string
    Annotations  []Annotation
}
```

The `AppName` field captures the GitHub App name from `checkSuite.app.name`. This is used by `FormatCheckName` as a fallback when `WorkflowName` is empty, allowing non-Actions checks (like GitHub Code Scanning, Checkov) to display as "GitHub Code Scanning / analyze" instead of just "analyze".

### PR Metadata Fetching (`internal/github/pr.go:144-170`)

Uses REST API for PR info and commit timestamps:

```go
func FetchPRInfo(ctx context.Context, client *github.Client, owner, repo string, prNumber int) (*PRInfo, error) {
    pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
    commit, _, err := client.Repositories.GetCommit(ctx, owner, repo, headSHA, nil)
    
    return &PRInfo{
        Number:         prNumber,
        Title:          pr.GetTitle(),
        HeadSHA:        headSHA,
        CreatedAt:      pr.GetCreatedAt().Format(TimestampFormat),
        HeadCommitDate: commit.GetCommit().GetCommitter().GetDate().Format(TimestampFormat),
    }, nil
}
```

Uses `TimestampFormat` from `internal/github/timestamp.go`.

### Forked Repository Handling (`internal/github/pr.go`)

**Problem**: When working on a forked repo, `git remote get-url origin` returns the fork's URL, not the upstream repository where the PR lives.

**Solution**: Use `gh pr view --json number,url` to get the PR URL, then extract owner/repo from that URL:

```go
func GetCurrentPRWithRepo() (int, string, string, error) {
    cmd := exec.Command("gh", "pr", "view", "--json", "number,url")
    output, err := cmd.Output()
    return parsePRViewWithRepo(output)
}

func parsePRViewWithRepo(jsonOutput []byte) (int, string, string, error) {
    // Parse JSON, then extract from URL like https://github.com/owner/repo/pull/123
    owner, repo, prNum, err := ParsePRURL(result.URL)
    return result.Number, owner, repo, nil
}
```

### Historical Job Averages (`internal/github/history.go`)

The application uses a **streaming approach** to fetch historical averages efficiently:

**Legacy Function** (`FetchJobAverages` at lines 31-153):

```go
func FetchJobAverages(
    ctx context.Context,
    client *github.Client,
    owner, repo string,
    checkRuns []CheckRunInfo,
    knownRunIDToWorkflowID map[int64]int64,
    knownFetchedWorkflowIDs map[int64]bool,
) (averages map[string]time.Duration, ...) {
    // Step 1: Extract run IDs from check run URLs
    // Step 2: Map run IDs to workflow IDs (using cache)
    // Step 3: Filter already-fetched workflow IDs
    // Step 4: Fetch recent completed runs for each workflow
    // Step 5: Collect job durations from each run
    // Step 6: Average durations per job name
}
```

**Streaming Functions** (added in issue #136):

```go
// DiscoverWorkflows resolves run IDs to workflow IDs.
// Returns new run ID → workflow ID mappings and the list of workflow IDs that need fetching.
func DiscoverWorkflows(
    ctx context.Context,
    client *github.Client,
    owner, repo string,
    checkRuns []CheckRunInfo,
    knownRunIDToWorkflowID map[int64]int64,
    knownFetchedWorkflowIDs map[int64]bool,
) (newRunIDToWorkflowID map[int64]int64, workflowIDsToFetch []int64, err error)

// FetchWorkflowHistory fetches historical job durations for a single workflow.
// Returns averaged durations per job name for the given workflow.
func FetchWorkflowHistory(
    ctx context.Context,
    client *github.Client,
    owner, repo string,
    workflowID int64,
) (map[string]time.Duration, error)
```

**Streaming Flow** (`internal/tui/update.go:253-287`):

1. `handleChecksUpdate()` detects checks have arrived
2. Waits for `historyFetchDelay` (10s) after first checks appear
3. Dispatches `discoverWorkflows()` command
4. `WorkflowsDiscoveredMsg` returns workflow IDs to fetch
5. For each workflow ID, dispatches `fetchWorkflowHistory()` command
6. Each `JobAveragesPartialMsg` merges results incrementally
7. When `pendingWorkflowFetch` is empty, discovery phase completes

**Incremental Caching**: The `runIDToWorkflowID`, `fetchedWorkflowIDs`, `pendingWorkflowFetch`, and `dispatchedWorkflowFetch` maps prevent redundant API calls across polling cycles. Additionally, `expectedCheckCount` (derived from `len(m.jobAverages)`) feeds into the `canTrustCompletion()` premature exit prevention system.

### Helper Modules

#### `internal/github/conclusion.go`

```go
func FailureConclusion(conclusion string) bool {
    return conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required"
}
```

Simple helper that centralizes failure conclusion logic.

#### `internal/github/timestamp.go`

```go
const TimestampFormat = "2006-01-02T15:04:05Z"

func ParseTimestamp(s string) (time.Time, error) {
    return time.Parse(TimestampFormat, s)
}
```

Centralized timestamp format for parsing GitHub API timestamps.

---

## 7. Timing Calculations

The `internal/timing/calculator.go` module provides three core metrics.

### Queue Latency (`calculator.go:11-16`)

```go
func QueueLatency(commitTime time.Time, check ghclient.CheckRunInfo) time.Duration {
    if check.StartedAt == nil || commitTime.IsZero() {
        return 0
    }
    return check.StartedAt.Sub(commitTime)
}
```

**Measures**: Time from commit push to check start.

### Runtime (`calculator.go:19-24`)

```go
func Runtime(check ghclient.CheckRunInfo) time.Duration {
    if check.Status != "in_progress" || check.StartedAt == nil {
        return 0
    }
    return time.Since(*check.StartedAt)
}
```

**Measures**: Elapsed time for currently running checks.

### Final Duration (`calculator.go:27-32`)

```go
func FinalDuration(check ghclient.CheckRunInfo) time.Duration {
    if check.StartedAt == nil || check.CompletedAt == nil {
        return 0
    }
    return check.CompletedAt.Sub(*check.StartedAt)
}
```

**Measures**: Total runtime for completed checks.

### Duration Formatting (`calculator.go:35-57`)

**Output Examples**:

- 45s → `45s`
- 125s → `2m 5s`
- 3725s → `1h 2m 5s`

---

## 8. TUI Rendering System

### Constants (`internal/tui/constants.go`)

```go
const (
    slowJobThreshold     = 2 * time.Minute
    verySlowJobThreshold = 3 * time.Minute

    rateBackoffThreshold = 10
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
- `minRateLimitForFetch`: Minimum rate limit to fetch historical averages
- `historyFetchDelay`: Delay before starting historical average fetch (prevents premature API calls during check startup)
- `minCheckAppearanceRatio`: Minimum ratio of seen checks to expected checks (30%) before trusting completion (prevents premature exit when only fast checks like DCO have appeared)
- `startupGracePeriod`: Maximum time to wait before trusting completion regardless of check counts (2 minutes)

### View Function (`internal/tui/view.go`)

Renders the entire UI every frame, including historical averages status and premature exit prevention messages:

```go
func (m Model) View() tea.View {
    // ... header with PR title and averages status
    
    // ... check run rendering with error boxes
    
    // Premature exit prevention message
    if allChecksComplete(m.checkRuns) && !canTrustCompletion(&m) {
        b.WriteString(m.styles.Queued.Render("  ⏳ Waiting for more checks to appear...\n"))
        if m.expectedCheckCount > 0 {
            fmt.Fprintf(&b, m.styles.Queued.Render("  Seen %d of ~%d expected checks (%d%% threshold: %d%%)\n"),
                len(m.checkRuns), m.expectedCheckCount,
                int(minCheckAppearanceRatio*100),
                int(float64(len(m.checkRuns))/float64(m.expectedCheckCount)*100))
        } else {
            elapsed := time.Since(m.firstCheckSeenAt)
            remaining := startupGracePeriod - elapsed
            if remaining > 0 {
                fmt.Fprintf(&b, m.styles.Queued.Render("  Grace period: %s remaining\n"),
                    timing.FormatDuration(remaining))
            }
        }
        b.WriteString("\n")
    }
    
    // Rate limit warning
    if m.rateLimitRemaining < minRateLimitForFetch {
        b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
    }
}
```

**Premature Exit Display**: When all visible checks are complete but `canTrustCompletion()` returns false, the TUI shows a "Waiting for more checks to appear..." message with either the appearance ratio (if `expectedCheckCount` is available from history) or a grace period countdown. This prevents the user from seeing a brief "all passed" state followed by new checks appearing.

### Header Format (`internal/tui/display.go`)

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

- "Start" (was "Queue") - Queue latency
- "Workflow/Job" - Check name (may show "App / Job" for GHAS or third-party checks)
- "ThisRun" - Current run duration
- "HistAvg" - Historical average duration

### Check Name Formatting (`internal/tui/display.go`)

`FormatCheckName` now supports three tiers of name formatting:

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

`FormatCheckNameWithTruncate` follows the same priority for truncation, preserving the prefix and truncating only the job name.

### Error Annotation Display (`internal/tui/view.go:96-130`)

```go
func (m Model) renderErrorBox(check ghclient.CheckRunInfo, widths ColumnWidths) string {
    var b strings.Builder

    for _, ann := range check.Annotations {
        var errorMsg string
        if ann.Message != "" {
            errorMsg = ann.Message
            if ann.Title != "" {
                errorMsg = ann.Title + ": " + errorMsg
            }
        } else if ann.Title != "" {
            errorMsg = ann.Title
        } else {
            continue
        }

        if ann.Path != "" {
            if ann.StartLine > 0 {
                errorMsg = fmt.Sprintf("%s:%d - %s", ann.Path, ann.StartLine, errorMsg)
            } else {
                errorMsg = fmt.Sprintf("%s - %s", ann.Path, errorMsg)
            }
        }
        b.WriteString("  ")
        b.WriteString(m.styles.ErrorBox.Render(errorMsg))
        b.WriteString("\n")
    }

    if b.Len() > 0 {
        b.WriteString("\n")
    }

    return b.String()
}
```

Annotations are fetched directly via GraphQL and include path, line number, title, and message.

---

## 9. Error Handling & Edge Cases

### Network Errors During Polling (`internal/tui/update.go:118-121`)

```go
if msg.Err != nil {
    m.err = msg.Err
    return m, nil  // Continue polling, don't quit
}
```

**Design Decision**: Network errors are non-fatal. The TUI displays the error but continues polling.

### Rate Limit Handling (`internal/tui/update.go:39-41`)

```go
if m.rateLimitRemaining < rateBackoffThreshold {
    return m, tick(m.refreshInterval * 3)  // Back off to 15s
}
```

**Backoff Strategy**: When remaining API calls < 10, poll interval triples from 5s to 15s.

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

---

## 10. Data Flow Diagrams

### Snapshot Mode Flow

```text
main.go:185 run()
    │
    ├── config.Load()
    │   └── Returns: Config{RefreshInterval, Colors, EnableLinks}
    │
    ├── tui.NewStyles()
    │   └── Returns: Styles{Success, Failure, Running, Queued, Info, Warning, ...}
    │
    ├── Determine PR (URL, number, or auto-detect)
    │   ├── URL: ghclient.ParsePRURL()
    │   ├── Number: ghclient.GetPRWithRepo()
    │   └── Auto: ghclient.GetCurrentPRWithRepo()
    │       └── Runs: gh pr view --json number,url
    │
    ├── ghclient.GetToken()
    │
    ├── Check terminal: term.IsTerminal()
    │   └── FALSE: Run snapshot mode
    │
    └── runSnapshot()                               [main.go:50]
        │
        ├── ghclient.NewClient()
        │   └── Creates REST API client with OAuth2
        │
        ├── ghclient.FetchPRInfo()                  [internal/github/pr.go:144]
        │   └── Returns: PRInfo{Title, HeadSHA, HeadCommitDate}
        │
        ├── ghclient.FetchCheckRunsGraphQL()        [internal/github/graphql.go:94]
        │   └── Returns: []CheckRunInfo{Name, WorkflowName, Status, ...}
        │
        ├── ghclient.FetchJobAverages() (unless --quick)
        │   └── Returns: map[jobName]averageDuration
        │
        ├── tui.CalculateColumnWidths()
        │   └── Returns: ColumnWidths{Queue, Name, Duration, Avg}
        │
        └── Render output
            ├── tui.FormatHeaderColumns()
            ├── tui.BuildNameColumn()
            ├── tui.FormatQueueLatency()
            ├── tui.FormatDuration()
            ├── tui.FormatAvg()
            └── Determine exit code (ghclient.FailureConclusion())
```

### TUI Mode Flow

```text
main.go:207 (TUI mode)
    │
    ├── tui.NewModel()                              [internal/tui/model.go:70]
    │   └── Returns: Model{ctx, token, owner, repo, prNumber, spinner, ...}
    │       - Initializes empty maps: jobAverages, runIDToWorkflowID, 
    │         fetchedWorkflowIDs, pendingWorkflowFetch, dispatchedWorkflowFetch
    │       - expectedCheckCount = 0, peakCheckCount = 0
    │
    ├── tea.NewProgram(model)
    │   └── Creates program with model
    │
    └── p.Run()                                     [Blocking event loop]
        │
        └── model.Init()                            [internal/tui/update.go:56]
            │
            ├── Returns: tea.Batch(
            │       spinner.Tick,
            │       fetchPRInfo(),
            │       tick(m.refreshInterval)
            │   )
            │
            └── Message processing loop
                │
                ├── [PRInfoMsg received]
                │   ├── Store: prTitle, headSHA, headCommitTime
                │   └── Return: fetchCheckRuns()
                │
                ├── [TickMsg received]
                │   ├── Check: rateLimitRemaining < 10?
                │   │   └── YES: Back off to 15s interval
                │   └── Return: tea.Batch(
                │           fetchCheckRuns(),
                │           tick(m.refreshInterval)
                │       )
                │
                ├── [ChecksUpdateMsg received]
                │   └── handleChecksUpdate()
                │       ├── SortCheckRuns() by duration
                │       ├── Track peakCheckCount (max checks seen)
                │       ├── Track firstCheckSeenAt (when checks first appear)
                │       ├── Check: elapsed >= historyFetchDelay OR allComplete?
                │       │   └── YES: Dispatch discoverWorkflows()
                │       ├── Check: allChecksComplete && canTrustCompletion?
                │       │   ├── canTrustCompletion checks:
                │       │   │   1. Grace period elapsed? → trust
                │       │   │   2. Checks disappeared (current < peak)? → don't trust
                │       │   │   3. Appearance ratio >= 30%? → trust
                │       │   │   4. No expected count & no grace period? → don't trust
                │       │   └── If trusted: set exitCode, mark checksComplete
                │       └── If not trusted: display "Waiting for more checks..."
                │
                ├── [WorkflowsDiscoveredMsg received]
                │   ├── Store: runIDToWorkflowID mappings
                │   ├── For each workflowID in WorkflowIDsToFetch:
                │   │   ├── Mark: pendingWorkflowFetch[wfID] = true
                │   │   ├── Mark: dispatchedWorkflowFetch[wfID] = true
                │   │   └── Dispatch: fetchWorkflowHistory(wfID)
                │   └── If no fetches: discovery phase complete
                │
                ├── [JobAveragesPartialMsg received]
                │   ├── Remove from: pendingWorkflowFetch
                │   ├── Mark in: fetchedWorkflowIDs
                │   ├── Merge averages into: jobAverages
                │   ├── Update: expectedCheckCount = len(jobAverages)
                │   └── If pendingWorkflowFetch empty: discovery complete
                │
                ├── [spinner.TickMsg received]
                │   └── Update spinner animation
                │
                └── [tea.KeyMsg received]
                    └── If "q" or "ctrl+c": tea.Quit
```

### Forked PR Detection Flow

```text
GetPRWithRepo() or GetCurrentPRWithRepo()
    │
    ├── Exec: gh pr view --json number,url
    │
    ├── Parse JSON: {number, url}
    │
    └── ParsePRURL(url)
        └── Extract owner, repo from URL
            └── Returns: owner, repo, prNumber (from upstream repo, not fork)
```

---

## 11. Exit Behavior

### Exit Codes

| Code | Meaning | Example |
| ---- | ------- | ------- |
| 0 | Success | All checks passed |
| 0 | No checks | PR has no workflows (snapshot mode) |
| 0 | Incomplete checks | Checks still running (snapshot mode) |
| 1 | Check failure | One or more checks failed |
| 1 | Authentication error | Missing GITHUB_TOKEN |
| 1 | Network error | Failed to fetch PR info (TUI mode initialization) |
| 1 | Invalid input | Bad PR number or URL argument |

### Exit Code Determination

```go
func determineExitCode(checks []ghclient.CheckRunInfo) int {
    for _, check := range checks {
        if ghclient.FailureConclusion(check.Conclusion) {
            return 1
        }
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

Even when `allChecksComplete()` returns `true`, the TUI applies an additional `canTrustCompletion()` gate (see [Completion Gate](#completion-gate-cantrustcompletion)) to prevent exiting before all expected checks have appeared. When completion can't be trusted, the TUI displays "Waiting for more checks to appear..." with either an appearance ratio or grace period countdown.

### Clean Shutdown

TUI mode exits cleanly by:

1. Setting `m.quitting = true`
2. Returning `tea.Quit` command
3. Bubbletea restores terminal state
4. Final model passed back to `main()`
5. Exit code extracted from model
6. `os.Exit(exitCode)` terminates process

---

## Summary

gh-observer demonstrates several best practices:

1. **Clean separation of concerns**: Distinct packages for config, GitHub API, timing, and TUI rendering
2. **Efficient API usage**: GraphQL for complex queries, REST for simple metadata
3. **Graceful error handling**: Non-fatal errors during polling, fatal errors at initialization
4. **Terminal-aware output**: Snapshot mode for CI, TUI mode for interactive use
5. **Rate limit awareness**: Backoff strategy and remaining quota display
6. **Streaming data fetching**: Historical averages fetched per-workflow to reduce latency and provide early feedback
7. **User feedback**: Startup phase messaging, real-time updates, fetch progress display
8. **Fork support**: Correctly identifies upstream repository for forked PRs
9. **Delayed fetching**: Waits 10 seconds after first checks appear before fetching historical averages
10. **Concurrent coordination**: Uses pending/dispatched tracking to coordinate multiple async fetches
11. **Premature exit prevention**: Uses `canTrustCompletion()` with grace period, appearance ratio, and peak tracking to prevent exiting when fast checks complete before others appear
12. **GHAS and third-party app detection**: Uses `AppName` from `checkSuite.app` to provide meaningful names for non-Actions checks like GitHub Code Scanning and Bridgecrew

The codebase follows the Elm Architecture pattern through Bubbletea, making the state management predictable and testable. The linear execution flow from initialization through polling to exit is clear and well-structured.

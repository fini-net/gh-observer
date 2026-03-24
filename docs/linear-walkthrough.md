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
  - `--slow-nonerror`: Show logs for successful jobs running longer than 1 minute
- **Execution**: Calls `run(args)` and exits with the returned exit code

```go
var quickFlag bool
var slowNonerrorFlag bool

func init() {
    rootCmd.Flags().BoolVarP(&quickFlag, "quick", "q", false, "Skip fetching historical average runtimes")
    rootCmd.Flags().BoolVar(&slowNonerrorFlag, "slow-nonerror", false, "Show logs for successful jobs running longer than 1 minute")
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

### URL Support (`main.go:203-239`)

The application accepts either a PR number or a full GitHub PR URL:

```go
if len(args) > 0 {
    arg := args[0]
    // Check if argument is a PR URL
    if strings.Contains(arg, "github.com") && strings.Contains(arg, "/pull/") {
        owner, repo, prNumber, err = ghclient.ParsePRURL(arg)
    } else {
        // PR number provided: use gh pr view to get correct repo (handles forks)
        n, err := strconv.Atoi(arg)
        prNumber, owner, repo, err = ghclient.GetPRWithRepo(n)
    }
} else {
    // Auto-detect from current branch (correctly handles forks)
    prNumber, owner, repo, err = ghclient.GetCurrentPRWithRepo()
}
```

**Why use PR URL?** External repositories can be watched without cloning them locally. The URL contains all the information needed (owner, repo, PR number).

**Fork Handling**: The code uses `GetPRWithRepo()` and `GetCurrentPRWithRepo()` instead of `ParseOwnerRepo()` to correctly identify the repository for forked PRs. The local git remote might point to a fork, but the PR lives in the upstream repository.

### Main Run Function (`main.go:185-270`)

The `run()` function orchestrates all initialization and mode selection:

#### Step 1: Configuration Loading (`main.go:188-193`)

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

#### Step 2: Style Creation (`main.go:196-201`)

```go
styles := tui.NewStyles(
    cfg.Colors.Success,
    cfg.Colors.Failure,
    cfg.Colors.Running,
    cfg.Colors.Queued,
)
```

Creates Lipgloss styles for rendering colored output. See `internal/tui/styles.go` for implementation.

#### Step 3: PR Resolution (`main.go:203-238`)

Two scenarios:

**Explicit PR number**: Uses `GetPRWithRepo(n)` to get owner/repo from the PR URL (handles forks correctly).

**Explicit PR URL**: Uses `ParsePRURL(url)` to extract owner/repo/number directly.

**Auto-detection**: Uses `GetCurrentPRWithRepo()` which calls `gh pr view --json number,url` and extracts the correct repository from the PR URL.

#### Step 4: Authentication (`main.go:240-245`)

```go
token, err := ghclient.GetToken()
```

Located at `internal/github/client.go`. Token acquisition strategy:

1. **First**: Check `GITHUB_TOKEN` environment variable
2. **Fallback**: Run `gh auth token` command
3. **Error**: Return message if both fail

#### Step 5: Mode Selection (`main.go:247-251`)

```go
if !term.IsTerminal(int(os.Stdout.Fd())) {
    return runSnapshot(ctx, token, owner, repo, prNumber, cfg.EnableLinks, quickFlag, slowNonerrorFlag)
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

Uses `google/go-github/v84` library with OAuth2 token authentication.

### GraphQL Client Creation (`internal/github/graphql.go:94-98`)

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

### Implementation (`main.go:50-183`)

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

Located at `internal/github/history.go:41-162`. Fetches recent completed workflow runs to calculate average job durations.

#### Step 5: Fetch Slow Job Logs (if `--slow-nonerror`)

```go
jobSlowLogs := make(map[int64][]string)
if slowNonerror {
    for _, check := range checkRuns {
        // Only for in_progress or completed success with runtime > 1 minute
        ...
        lines, err := ghclient.FetchLastNJobLines(ctx, client, owner, repo, jobID, 5)
    }
}
```

Located at `internal/github/logs.go:225-306`. Fetches the last N lines from job logs.

#### Step 6: Calculate Column Widths

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

### Model Creation (`main.go:254`)

```go
model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag, slowNonerrorFlag)
```

Located at `internal/tui/model.go:81-107`. Initializes the Bubbletea model:

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
    jobAverages          map[string]time.Duration
    runIDToWorkflowID    map[int64]int64
    fetchedWorkflowIDs   map[int64]bool
    avgFetchPending      bool
    avgFetchStartTime    time.Time
    avgFetchLastDuration time.Duration
    avgFetchErr          error
    noAvg                bool
    
    // Job log errors (fetched async for failed checks)
    jobLogErrors    map[int64][]string
    logFetchPending map[int64]bool
    
    // Slow non-error job logs
    slowNonerror        bool
    jobSlowLogs         map[int64][]string
    slowLogFetchPending map[int64]bool
    slowLogLastFetch    map[int64]time.Time
    
    // UI state
    spinner         spinner.Model
    startTime       time.Time
    lastUpdate      time.Time
    refreshInterval time.Duration
    styles          Styles
    
    // Exit tracking
    exitCode int
    quitting bool
    checksComplete bool
    
    // Error state
    err error
    
    // Feature flags
    enableLinks bool
}
```

### Program Initialization (`main.go:257-262`)

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

type JobAveragesMsg struct {         // Historical averages received
    Averages              map[string]time.Duration
    NewRunIDToWorkflowID  map[int64]int64
    NewFetchedWorkflowIDs []int64
    Err                   error
}

type JobLogMsg struct {              // Failed job logs received
    JobID  int64
    Errors []string
    Err    error
}

type SlowJobLogMsg struct {          // Slow job logs received
    JobID int64
    Lines []string
    Err   error
}

type ErrorMsg struct {               // Error occurred
    Err error
}
```

### Update Function (`internal/tui/update.go:23-114`)

The `Update()` method handles all incoming messages:

#### Message: Keyboard Input (`update.go:25-30`)

```go
case tea.KeyMsg:
    switch msg.String() {
    case "q", "ctrl+c":
        m.quitting = true
        return m, tea.Quit
    }
```

#### Message: Poll Timer (`update.go:37-48`)

```go
case TickMsg:
    if m.rateLimitRemaining < rateBackoffThreshold {
        return m, tick(m.refreshInterval * 3)  // Back off
    }
    
    return m, tea.Batch(
        fetchCheckRuns(m.ctx, m.token, m.owner, m.repo, m.prNumber),
        tick(m.refreshInterval),
    )
```

**Rate Limiting**: Uses constant `rateBackoffThreshold` (10) from `internal/tui/constants.go`.

#### Message: PR Info Received (`update.go:50-62`)

```go
case PRInfoMsg:
    if msg.Err != nil {
        m.err = msg.Err
        return m, tea.Quit
    }
    
    m.prTitle = msg.Title
    m.headSHA = msg.HeadSHA
    m.prCreatedAt = msg.CreatedAt
    m.headCommitTime = msg.HeadCommitTime
    
    return m, fetchCheckRuns(m.ctx, m.token, m.owner, m.repo, m.prNumber)
```

#### Message: Check Runs Updated (`update.go:64-66`)

Delegates to `handleChecksUpdate()` for clarity:

```go
case ChecksUpdateMsg:
    return m.handleChecksUpdate(msg)
```

#### Message: Job Averages Received (`update.go:67-89`)

```go
case JobAveragesMsg:
    m.avgFetchPending = false
    m.avgFetchLastDuration = time.Since(m.avgFetchStartTime)

    if msg.Err != nil {
        m.avgFetchErr = msg.Err
    } else {
        maps.Copy(m.jobAverages, msg.Averages)
        maps.Copy(m.runIDToWorkflowID, msg.NewRunIDToWorkflowID)
        for _, wfID := range msg.NewFetchedWorkflowIDs {
            m.fetchedWorkflowIDs[wfID] = true
        }
    }

    if m.checksComplete {
        m.quitting = true
        return m, tea.Quit
    }
    return m, nil
```

**Incremental Caching**: The `runIDToWorkflowID` and `fetchedWorkflowIDs` maps prevent redundant API calls across polling cycles.

#### Message: Job Log Received (`update.go:91-106`)

For failed checks and slow jobs, logs are fetched asynchronously:

```go
case JobLogMsg:
    delete(m.logFetchPending, msg.JobID)
    if msg.Err == nil && len(msg.Errors) > 0 {
        m.jobLogErrors[msg.JobID] = msg.Errors
    }
    return m, nil

case SlowJobLogMsg:
    delete(m.slowLogFetchPending, msg.JobID)
    m.slowLogLastFetch[msg.JobID] = time.Now()
    if msg.Err == nil && len(msg.Lines) > 0 {
        m.jobSlowLogs[msg.JobID] = msg.Lines
    }
    return m, nil
```

### handleChecksUpdate (`internal/tui/update.go:117-166`)

The check update logic is refactored into a dedicated method:

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

    var cmds []tea.Cmd

    // Fetch historical averages for new workflows
    if !m.noAvg && !m.avgFetchPending && m.rateLimitRemaining >= minRateLimitForFetch {
        // ... fetch logic
    }

    // Fetch logs for failed and slow checks
    cmds = append(cmds, m.fetchLogsForFailedChecks(msg.CheckRuns)...)
    cmds = append(cmds, m.fetchLogsForSlowChecks(msg.CheckRuns)...)

    if allChecksComplete(m.checkRuns) {
        m.exitCode = determineExitCode(m.checkRuns)
        m.checksComplete = true
        if !m.avgFetchPending {
            m.quitting = true
            cmds = append(cmds, tea.Quit)
        }
        return m, tea.Batch(cmds...)
    }

    return m, tea.Batch(cmds...)
}
```

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

### Exit Code Determination (`internal/tui/update.go:359-366`)

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

---

## 6. GitHub API Layer Deep Dive

### GraphQL Query Structure (`internal/github/graphql.go:35-91`)

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

The `FetchJobAverages()` function calculates ETA estimates:

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

**Incremental Caching**: Returns `newRunIDToWorkflowID` and `newFetchedWorkflowIDs` for the caller to cache, avoiding redundant API calls across polling cycles.

### Job Log Fetching (`internal/github/logs.go`)

Two functions for fetching job logs:

#### `FetchJobLogs()` - Error Context for Failed Checks

```go
func FetchJobLogs(ctx context.Context, client *github.Client, owner, repo string, jobID int64) ([]string, error) {
    logURL, _, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 0)
    // Follow redirect and parse
    return parseErrorLines(resp.Body), nil
}
```

`parseErrorLines()` extracts up to 3 relevant error lines:

1. Lines containing `##[error]` markers
2. Shell error patterns (`command not found`, `No such file or directory`)
3. Context from preceding lines for generic exit code errors

#### `FetchLastNJobLines()` - Last N Lines for Slow Jobs

```go
func FetchLastNJobLines(ctx context.Context, client *github.Client, owner, repo string, jobID int64, n int) ([]string, error) {
    // Uses ring buffer for O(N) memory
    return parseLastNLines(resp.Body, n), nil
}
```

Used when `--slow-nonerror` flag is set to show recent log lines for jobs running > 1 minute.

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
    slowLogRuntimeMin    = time.Minute
    slowLogFetchInterval = 10 * time.Second

    rateBackoffThreshold = 10
    minRateLimitForFetch = 100
)
```

**Threshold Explanations**:

- `slowJobThreshold`: Time before showing "Still waiting" message
- `verySlowJobThreshold`: Time before showing "No checks found" message
- `slowLogRuntimeMin`: Minimum runtime before fetching slow job logs
- `slowLogFetchInterval`: Cooldown between slow log fetches
- `rateBackoffThreshold`: Remaining API calls before tripling poll interval
- `minRateLimitForFetch`: Minimum rate limit to fetch historical averages/logs

### View Function (`internal/tui/view.go:14-98`)

Renders the entire UI every frame, now including historical averages:

```go
func (m Model) View() tea.View {
    // ... header with PR title and averages status
    
    // Column headers now include 4th column
    headerQueue, headerName, headerDuration, headerAvg := FormatHeaderColumns(widths)
    b.WriteString(m.styles.Header.Render(fmt.Sprintf("%s   %s  %s  %s\n", headerQueue, headerName, headerDuration, headerAvg)))
    
    for _, check := range m.checkRuns {
        checkLine := m.renderCheckRun(check, widths)
        b.WriteString(checkLine)
        
        // Error context for failed checks
        if check.Conclusion == "failure" || check.Conclusion == "timed_out" {
            b.WriteString(m.renderErrorBox(check, widths))
        }
        
        // Slow job logs (if --slow-nonerror)
        if m.slowNonerror {
            b.WriteString(m.renderSlowJobLogs(check, widths))
        }
    }
    
    // Rate limit warning
    if m.rateLimitRemaining < minRateLimitForFetch {
        b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
    }
}
```

### Header Format (`internal/tui/display.go:218-234`)

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
- "Workflow/Job" - Check name
- "ThisRun" - Current run duration
- "HistAvg" - Historical average duration

### Error Annotation Display (`internal/tui/view.go:100-156`)

```go
func (m Model) renderErrorBox(check ghclient.CheckRunInfo, widths ColumnWidths) string {
    // Separate annotations by level (failure vs warning)
    var failures, warnings []ghclient.Annotation
    for _, ann := range check.Annotations {
        switch ann.AnnotationLevel {
        case "failure":
            failures = append(failures, ann)
        case "warning":
            warnings = append(warnings, ann)
        }
    }
    
    // Render FAILURE annotations first (prominent)
    // Then job log errors (from FetchJobLogs)
    // Then WARNING annotations (dimmed)
}
```

**Example Output**:

```text
1m 5s  ✗  Test Suite / unit tests  5m 30s
  ┃ src/parser_test.go:42 - undefined: TestData
  ┃ From job logs:
  ┃   Process completed with exit code 1
```

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
        ├── ghclient.FetchLastNJobLines() (if --slow-nonerror)
        │   └── Returns last N lines for slow jobs
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
main.go:254 (TUI mode)
    │
    ├── tui.NewModel()                              [internal/tui/model.go:81]
    │   └── Returns: Model{ctx, token, owner, repo, prNumber, spinner, ...}
    │
    ├── tea.NewProgram(model)
    │   └── Creates program with model
    │
    └── p.Run()                                     [Blocking event loop]
        │
        └── model.Init()                            [internal/tui/update.go:14]
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
                │       ├── Fetch historical averages (if new workflows)
                │       ├── Fetch logs for failed checks
                │       ├── Fetch logs for slow checks (--slow-nonerror)
                │       └── Check completion → exit
                │
                ├── [JobAveragesMsg received]
                │   ├── Store averages in model
                │   └── If checks complete: quit
                │
                ├── [JobLogMsg received]
                │   └── Store error lines for failed check
                │
                ├── [SlowJobLogMsg received]
                │   └── Store last N lines for slow check
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
6. **Incremental caching**: Historical averages cached efficiently across polling cycles
7. **User feedback**: Startup phase messaging, real-time updates, error context from logs
8. **Fork support**: Correctly identifies upstream repository for forked PRs
9. **Historical averages**: Provides ETA estimates from past job runtimes
10. **Log context**: Extracts meaningful errors from failed job logs

The codebase follows the Elm Architecture pattern through Bubbletea, making the state management predictable and testable. The linear execution flow from initialization through polling to exit is clear and well-structured.

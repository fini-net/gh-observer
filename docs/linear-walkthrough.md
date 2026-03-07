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

### Command Registration (`main.go:25-36`)

The application uses [Cobra](https://github.com/spf13/cobra) for CLI argument parsing. The root command is registered with:

- **Usage**: `gh-observer [PR_NUMBER]`
- **Arguments**: Maximum of 1 argument (optional PR number)
- **Execution**: Calls `run(args)` and exits with the returned exit code

```go
var rootCmd = &cobra.Command{
    Use:   "gh-observer [PR_NUMBER]",
    Short: "Watch GitHub PR checks with runtime metrics",
    Args:  cobra.MaximumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        exitCode := run(args)
        os.Exit(exitCode)
    },
}
```

**Design Decision**: The exit code is captured and passed to `os.Exit()` explicitly. This allows the TUI to clean up properly before exiting.

### Main Run Function (`main.go:113-189`)

The `run()` function orchestrates all initialization and mode selection:

#### Step 1: Configuration Loading (`main.go:117-121`)

```go
cfg, err := config.Load()
```

Calls `internal/config/config.go:24` which:

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

**Config Structure** (`internal/config/config.go:18-22`):

```go
type Config struct {
    RefreshInterval time.Duration `mapstructure:"refresh_interval"`
    Colors          ColorConfig   `mapstructure:"colors"`
    EnableLinks     bool          `mapstructure:"enable_links"`
}
```

#### Step 2: Style Creation (`main.go:124-129`)

```go
styles := tui.NewStyles(
    cfg.Colors.Success,
    cfg.Colors.Failure,
    cfg.Colors.Running,
    cfg.Colors.Queued,
)
```

Creates Lipgloss styles for rendering colored output. See [`internal/tui/styles.go:23`](internal/tui/styles.go:23) for implementation.

#### Step 3: PR Number Resolution (`main.go:132-150`)

Two scenarios:

**Explicit PR Number** (provided as argument):

```go
n, err := strconv.Atoi(args[0])
prNumber = n
```

**Auto-detection** (no argument):

```go
n, err := ghclient.GetCurrentPR()
```

Calls `internal/github/pr.go:25` which runs:

```bash
gh pr view --json number --jq .number
```

**Why use `gh` CLI?** Parsing git branches to find PR numbers would require querying GitHub's API anyway. The `gh` CLI already has cached authentication and knows the current branch→PR mapping.

#### Step 4: Repository Identification (`main.go:153-157`)

```go
owner, repo, err := ghclient.ParseOwnerRepo()
```

Located at `internal/github/pr.go:41`. Workflow:

1. Runs `git remote get-url origin`
2. Parses the URL using regex patterns:
   - SSH: `git@github.com:owner/repo.git`
   - HTTPS: `https://github.com/owner/repo.git`
3. Returns `(owner, repo, error)`

**Implementation Detail** (`internal/github/pr.go:52-66`):

- Uses two regex patterns with optional `.git` suffix
- Trims trailing slashes for normalization
- Returns clear error if URL format unrecognized

#### Step 5: Authentication (`main.go:160-164`)

```go
token, err := ghclient.GetToken()
```

Located at `internal/github/client.go:15`. Token acquisition strategy:

1. **First**: Check `GITHUB_TOKEN` environment variable
2. **Fallback**: Run `gh auth token` command
3. **Error**: Return message if both fail

**Design Decision**: Environment variables take priority so CI/CD systems can override the `gh` CLI auth.

#### Step 6: Mode Selection (`main.go:167-170`)

```go
if !term.IsTerminal(int(os.Stdout.Fd())) {
    return runSnapshot(ctx, token, owner, repo, prNumber, cfg.EnableLinks)
}
```

Uses `golang.org/x/term` to detect if stdout is a terminal:

- **Not a terminal** (piped, redirected, or CI): Runs snapshot mode
- **Is a terminal**: Runs interactive TUI mode

**Why detect terminal?** Snapshot mode produces plain text without colors or TUI controls, making it suitable for scripts and CI pipelines. TUI mode provides rich interactive experience.

---

## 2. GitHub Authentication & Setup

### REST API Client Creation (`internal/github/client.go:36-45`)

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
- **GraphQL for check runs**: Single query fetches workflow name + job name + status + timestamps. Equivalent REST calls would:
  1. Get commit SHA from PR
  2. List check runs for SHA (multiple API calls if paginated)
  3. For each check run, fetch check suite
  4. For each check suite, fetch workflow run
  5. For each workflow run, fetch workflow name
  
GraphQL consolidates this into **one query**.

---

## 3. Execution Path A: Snapshot Mode

Snapshot mode runs when stdout is not a terminal (e.g., scripts, CI, redirected output).

### Implementation (`main.go:39-111`)

#### Step 1: Fetch PR Metadata (`main.go:41-59`)

```go
client, err := ghclient.NewClient(ctx)
prInfo, err := ghclient.FetchPRInfo(ctx, client, owner, repo, prNumber)
headCommitTime, err := time.Parse(time.RFC3339, prInfo.HeadCommitDate)
```

Located at `internal/github/pr.go:69`. Uses REST API to get:

- PR title
- Head SHA
- PR creation timestamp
- **Head commit timestamp** (needed for queue latency calculation)

**Why head commit timestamp?** Queue latency = `check.StartedAt - headCommitTime`. The PR creation time shows when the PR was opened, but the last commit time shows when code was pushed. GitHub Actions queueing starts from the push time.

#### Step 2: Fetch Check Runs (`main.go:62-66`)

```go
checkRuns, _, err := ghclient.FetchCheckRunsGraphQL(ctx, token, owner, repo, prNumber)
```

Returns `[]CheckRunInfo` with enriched data (see `internal/github/graphql.go:22-32`):

```go
type CheckRunInfo struct {
    Name         string
    WorkflowName string  // Enriched via GraphQL
    Summary      string
    Status       string
    Conclusion   string
    StartedAt    *time.Time
    CompletedAt  *time.Time
    DetailsURL   string
    Annotations  []Annotation
}
```

#### Step 3: Handle Empty Checks (`main.go:72-77`)

```go
if len(checkRuns) == 0 {
    sinceCreation := time.Since(headCommitTime)
    fmt.Printf("No checks found (commit pushed %s ago)\n", timing.FormatDuration(sinceCreation))
    fmt.Println("Checks may still be starting up or not configured for this PR")
    return 0
}
```

Returns exit code 0 (success) even with no checks, as this isn't a failure condition.

#### Step 4: Calculate Column Widths (`main.go:80`)

```go
widths := tui.CalculateColumnWidths(checkRuns, headCommitTime)
```

Located at `internal/tui/display.go:126`. Algorithm:

1. Initialize with minimum widths:
   - Queue: 5 chars
   - Name: 20 chars
   - Duration: 5 chars
2. Scan all check runs
3. Update widths to fit longest values
4. Cap name width at 60 chars (truncate with `…` if needed)

#### Step 5: Render Output (`main.go:82-108`)

```go
headerQueue, headerName, headerDuration := tui.FormatHeaderColumns(widths)
fmt.Printf("%s   %s  %s\n\n", headerQueue, headerName, headerDuration)

for _, check := range checkRuns {
    nameCol := tui.BuildNameColumn(check, widths, enableLinks)
    queueText := tui.FormatQueueLatency(check, headCommitTime)
    durationText := tui.FormatDuration(check)
    icon := tui.GetCheckIcon(check.Status, check.Conclusion)
    
    queueCol, _, durationCol := tui.FormatAlignedColumns(...)
    fmt.Printf("%s %s %s  %s\n", queueCol, icon, nameCol, durationCol)
    
    // Determine exit code
    if check.Status == "completed" {
        if conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required" {
            exitCode = 1
        }
    }
}
```

**Output Format**:

```text
Startup     Workflow/Job        Duration

42s    ✓ Build / test          2m 15s
1m 5s  ◐ Lint / check            1m 3s
```

#### Step 6: Exit Code Determination

Snapshot mode returns:

- **0**: All checks passed, no checks, or not all checks complete
- **1**: Any check has conclusion `failure`, `timed_out`, or `action_required`

**Note**: In snapshot mode, incomplete checks (queued, in_progress) result in exit code 0, as the checks haven't failed yet.

---

## 4. Execution Path B: Interactive TUI Mode

TUI mode runs when stdout is a terminal, providing real-time updates.

### Model Creation (`main.go:173`)

```go
model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles, cfg.EnableLinks)
```

Located at `internal/tui/model.go:57`. Initializes the Bubbletea model:

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

**Why store context?** Used for API call cancellation if the user quits early.

**Why track start time?** Used to display "Startup Phase" message with elapsed time.

### Program Initialization (`main.go:176-178`)

```go
p := tea.NewProgram(model)
finalModel, err := p.Run()
```

Creates a Bubbletea program and enters the event loop. The `Run()` method blocks until the program exits.

### Exit Code Extraction (`main.go:184-188`)

```go
if m, ok := finalModel.(tui.Model); ok {
    return m.ExitCode()
}
return 0
```

Type assertion extracts the final model state, then calls `ExitCode()` method.

---

## 5. TUI Message Processing Loop

Bubbletea follows the Elm Architecture pattern: **Model → Update → View** loop.

### Initialization (`internal/tui/update.go:13-19`)

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(
        m.spinner.Tick,                                    // Start spinner animation
        fetchPRInfo(m.ctx, m.token, m.owner, m.repo, m.prNumber),  // Fetch PR metadata
        tick(m.refreshInterval),                           // Schedule first poll timer
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

type ErrorMsg struct {               // Error occurred
    Err error
}
```

### Update Function (`internal/tui/update.go:22-91`)

The `Update()` method handles all incoming messages:

#### Message: Keyboard Input (`update.go:24-29`)

```go
case tea.KeyMsg:
    switch msg.String() {
    case "q", "ctrl+c":
        m.quitting = true
        return m, tea.Quit
    }
```

User can quit at any time with `q` or `Ctrl+C`.

#### Message: Spinner Tick (`update.go:31-34`)

```go
case spinner.TickMsg:
    var cmd tea.Cmd
    m.spinner, cmd = m.spinner.Update(msg)
    return m, cmd
```

Updates spinner animation state.

#### Message: Poll Timer (`update.go:36-50`)

```go
case TickMsg:
    if m.rateLimitRemaining < 10 {
        return m, tick(m.refreshInterval * 3)  // Back off
    }
    
    return m, tea.Batch(
        fetchCheckRuns(m.ctx, m.token, m.owner, m.repo, m.prNumber),
        tick(m.refreshInterval),
    )
```

**Rate Limiting**: If remaining API calls < 10, triple the poll interval to avoid hitting rate limits.

**Two commands returned**:

1. Fetch check runs (GraphQL)
2. Schedule next tick

#### Message: PR Info Received (`update.go:52-64`)

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

Populates PR metadata and immediately fetches check runs (no need to wait for next tick).

#### Message: Check Runs Updated (`update.go:66-83`)

```go
case ChecksUpdateMsg:
    if msg.Err != nil {
        m.err = msg.Err
        return m, nil  // Don't quit, continue polling
    }
    
    m.checkRuns = msg.CheckRuns
    m.rateLimitRemaining = msg.RateLimitRemaining
    m.lastUpdate = time.Now()
    m.err = nil
    
    if allChecksComplete(m.checkRuns) {
        m.exitCode = determineExitCode(m.checkRuns)
        m.quitting = true
        return m, tea.Quit
    }
    
    return m, nil
```

**Critical Logic**:

1. Stores updated check runs
2. Triggers render (via return)
3. **Automatically exits** when all checks complete

**Why continue polling on errors?** Network blips shouldn't crash the watcher. The user can quit manually with `q` if needed.

### Check Completion Detection (`internal/tui/update.go:143-155`)

```go
func allChecksComplete(checks []ghclient.CheckRunInfo) bool {
    if len(checks) == 0 {
        return false  // No checks yet, keep polling
    }
    
    for _, check := range checks {
        if check.Status != "completed" {
            return false
        }
    }
    
    return true
}
```

**Important**: Returns `false` if no checks exist yet. Otherwise, an empty check list would cause immediate exit.

### Exit Code Determination (`internal/tui/update.go:158-165`)

```go
func determineExitCode(checks []ghclient.CheckRunInfo) int {
    for _, check := range checks {
        if check.Conclusion == "failure" || 
           check.Conclusion == "timed_out" || 
           check.Conclusion == "action_required" {
            return 1
        }
    }
    return 0
}
```

**Failure Conditions**:

- `failure`: Check explicitly failed
- `timed_out`: Check exceeded time limit
- `action_required`: Manual approval needed (treated as failure for CI purposes)

**Success Conditions**: `success`, `cancelled`, `skipped` all return 0.

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

**Design Insights**:

1. **`commits(last: 1)`**: Gets the most recent commit (head commit)
2. **`statusCheckRollup`**: Consolidates both check runs and legacy status contexts
3. **`__typename`**: Distinguishes between `CheckRun` (GitHub Actions) and `StatusContext` (legacy CI)
4. **`checkSuite.workflowRun.workflow.name`**: This is the key enrichment - gets the workflow name
5. **`annotations(first: 5)`**: Limits to 5 error annotations to avoid massive responses
6. **`rateLimit.remaining`**: Tracks API quota with every request

### Status Context Normalization (`internal/github/graphql.go:121-149`)

Legacy status contexts have different status values than check runs:

```go
state := strings.ToLower(statusContext.State)

switch state {
case "success":
    status = "completed"
    conclusion = "success"
case "error", "failure":
    status = "completed"
    conclusion = "failure"
case "pending":
    status = "queued"
    conclusion = ""
default:
    status = "queued"
    conclusion = ""
}
```

**Why normalize?** Check runs use `status: "completed", conclusion: "success"`, while status contexts use `state: "success"`. Normalizing to check run format simplifies the display logic.

### PR Metadata Fetching (`internal/github/pr.go:69-95`)

Uses REST API instead of GraphQL:

```go
pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
commit, _, err := client.Repositories.GetCommit(ctx, owner, repo, headSHA, nil)
```

**Why two API calls?** PR metadata doesn't include commit timestamps. We need to:

1. Get PR info (title, SHA, creation time)
2. Get commit info (committer timestamp)

**Why not GraphQL?** REST is simpler for these basic queries, and we only call it once at startup.

### REST Check Runs (Unused) (`internal/github/checks.go:16-36`)

The codebase includes `FetchCheckRuns()` using REST API, but it's not currently used. It remains as backup/reference:

```go
result, resp, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, opts)
```

This would require additional API calls to get workflow names, hence the GraphQL preference.

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

**Example**:

- User pushes commit at 12:00:00
- GitHub queues the job
- Check starts running at 12:00:42
- Queue latency = 42 seconds

**Why it matters**: Shows GitHub's queue delay. High values indicate CI is busy.

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

**Example**:

- Check started at 12:01:00
- Current time is 12:03:15
- Runtime = 2m 15s (updates every render)

**Only for in-progress checks**: Completed checks use final duration instead.

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

**Example**:

- Check started at 12:01:00
- Check completed at 12:03:30
- Final duration = 2m 30s (static value)

### Duration Formatting (`calculator.go:35-57`)

```go
func FormatDuration(d time.Duration) string {
    d = d.Round(time.Second)
    
    if d <= 0 {
        return "0s"
    }
    
    hours := d / time.Hour
    d -= hours * time.Hour
    minutes := d / time.Minute
    d -= minutes * time.Minute
    seconds := d / time.Second
    
    if hours > 0 {
        return formatParts(int(hours), "h", int(minutes), "m", int(seconds), "s")
    }
    if minutes > 0 {
        return formatParts(int(minutes), "m", int(seconds), "s", 0, "")
    }
    return formatParts(int(seconds), "s", 0, "", 0, "")
}
```

**Output Examples**:

- 45s → `45s`
- 125s → `2m 5s`
- 3725s → `1h 2m 5s`

**Rounding**: Rounds to nearest second to avoid showing milliseconds.

**Design Decision**: Uses space-separated format (`2m 5s`) instead of compact format (`2m5s`) for better readability.

---

## 8. TUI Rendering System

### View Function (`internal/tui/view.go:14-77`)

The `View()` method renders the entire UI every frame:

```go
func (m Model) View() tea.View {
    if m.err != nil {
        return tea.NewView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
    }
    
    var b strings.Builder
    
    // Header: PR title and timestamps
    if m.prTitle != "" {
        b.WriteString(...)
    }
    
    // Startup message or check list
    if len(m.checkRuns) == 0 {
        return tea.NewView(b.String() + m.renderStartupPhase())
    }
    
    widths := CalculateColumnWidths(m.checkRuns, m.headCommitTime)
    
    // Column headers
    b.WriteString(...)
    
    // Check runs
    for _, check := range m.checkRuns {
        b.WriteString(m.renderCheckRun(check, widths))
        
        // Show summary and errors for failed checks
        if check.Conclusion == "failure" || check.Conclusion == "timed_out" {
            b.WriteString(m.renderSummary(check, widths))
            b.WriteString(m.renderErrorBox(check, widths))
        }
    }
    
    // Rate limit warning
    if m.rateLimitRemaining < 100 {
        b.WriteString(...)
    }
    
    // Help text
    b.WriteString("\nPress q to quit\n")
    
    return tea.NewView(b.String())
}
```

### Column Width Calculation (`internal/tui/display.go:126-160`)

```go
func CalculateColumnWidths(checkRuns []ghclient.CheckRunInfo, headCommitTime time.Time) ColumnWidths {
    const (
        minNameWidth = 20
        maxNameWidth = 60
        minTimeWidth = 5
    )
    
    widths := ColumnWidths{
        QueueWidth:    minTimeWidth,
        NameWidth:     minNameWidth,
        DurationWidth: minTimeWidth,
    }
    
    for _, check := range checkRuns {
        queueText := FormatQueueLatency(check, headCommitTime)
        if len(queueText) > widths.QueueWidth {
            widths.QueueWidth = len(queueText)
        }
        
        name := FormatCheckName(check)
        nameLen := len(name)
        if nameLen > widths.NameWidth && nameLen <= maxNameWidth {
            widths.NameWidth = nameLen
        } else if nameLen > maxNameWidth {
            widths.NameWidth = maxNameWidth
        }
        
        durationText := FormatDuration(check)
        if len(durationText) > widths.DurationWidth {
            widths.DurationWidth = len(durationText)
        }
    }
    
    return widths
}
```

**Algorithm**:

1. Start with minimum widths
2. Scan all check runs
3. Expand columns to fit longest values
4. Cap name width at 60 chars

**Why scan all checks?** Column alignment requires consistent widths across all rows.

### Check Name Formatting (`internal/tui/display.go:79-98`)

```go
func FormatCheckName(check ghclient.CheckRunInfo) string {
    if check.WorkflowName != "" {
        return fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
    }
    return check.Name
}
```

**Display Patterns**:

- Modern check run: `"CI / test"` (workflow: CI, job: test)
- Legacy check run: `"Travis CI"` (no workflow, just name)

**Truncation** (`display.go:87-98`):

```go
func FormatCheckNameWithTruncate(check ghclient.CheckRunInfo, maxWidth int) string {
    name := FormatCheckName(check)
    if len(name) > maxWidth {
        ellipsis := "…"
        truncateAt := maxWidth - len(ellipsis)
        if truncateAt < 0 {
            truncateAt = 0
        }
        return name[:truncateAt] + ellipsis
    }
    return name
}
```

Long names like `"Deploy Production / Build and push Docker image"` become `"Deploy Production / Build and push Dock…"`

### Icon Mapping (`internal/tui/display.go:50-76`)

```go
func GetCheckIcon(status, conclusion string) string {
    switch status {
    case "completed":
        switch conclusion {
        case "success":    return "✓"
        case "failure":    return "✗"
        case "cancelled":  return "⊗"
        case "skipped":    return "⊘"
        case "timed_out":  return "⏱"
        case "action_required": return "!"
        default:           return "?"
        }
    case "in_progress": return "◐"
    case "queued":      return "⏸"
    default:            return "?"
    }
}
```

**Icon Rationale**:

- ✓ (checkmark): Success
- ✗ (cross mark): Failure
- ⊗ (circled times): Cancelled
- ⊘ (circled slash): Skipped
- ⏱ (timer): Timed out
- ! (exclamation): Needs action
- ◐ (circle half): In progress (looks like loading spinner)
- ⏸ (pause): Queued

### Terminal Hyperlinks (`internal/tui/display.go:101-106`)

```go
func FormatLink(url, text string) string {
    if url == "" {
        return text
    }
    return termenv.Hyperlink(url, text)
}
```

Uses [OSC 8 escape sequences](https://gist.github.com/egmontkob/eb114294efbcd5dda193c68a2e4b9c88) to create clickable links in supported terminals:

**Example**: `test` becomes a clickable link to `https://github.com/owner/repo/actions/runs/123`

**Fallback**: Unsupported terminals just display the text.

### Name Column Building (`internal/tui/display.go:112-123`)

```go
func BuildNameColumn(check ghclient.CheckRunInfo, widths ColumnWidths, enableLinks bool) string {
    name := FormatCheckNameWithTruncate(check, widths.NameWidth)
    paddingLen := widths.NameWidth - len(name)
    if paddingLen < 0 {
        paddingLen = 0
    }
    padding := strings.Repeat(" ", paddingLen)
    
    if enableLinks && check.DetailsURL != "" {
        return FormatLink(check.DetailsURL, name) + padding
    }
    return name + padding
}
```

**Critical Detail**: Padding is added **outside** the hyperlink. This ensures:

- Visible text is exactly `widths.NameWidth` characters
- Terminal hyperlink only wraps the name, not the trailing spaces
- Alignment works correctly with OSC 8 sequences

### Alignment Logic (`internal/tui/display.go:163-183`)

```go
func FormatAlignedColumns(queueText, nameText, durationText string, widths ColumnWidths) (string, string, string) {
    // Right-align queue latency
    queuePadding := widths.QueueWidth - len(queueText)
    if queuePadding < 0 {
        queuePadding = 0
    }
    queueCol := strings.Repeat(" ", queuePadding) + queueText
    
    // Left-align name
    namePadding := widths.NameWidth - len(nameText)
    if namePadding < 0 {
        namePadding = 0
    }
    nameCol := nameText + strings.Repeat(" ", namePadding)
    
    // Right-align duration
    durationPadding := widths.DurationWidth - len(durationText)
    if durationPadding < 0 {
        durationPadding = 0
    }
    durationCol := strings.Repeat(" ", durationPadding) + durationText
    
    return queueCol, nameCol, durationCol
}
```

**Column Alignment**:

- Queue latency: Right-aligned (numbers line up on right)
- Name: Left-aligned (text flows naturally)
- Duration: Right-aligned (numbers line up on right)

**Output Format**:

```text
Queue     Name                     Duration
─────────────────────────────────────────────
  42s  ✓  Build / test           2m 15s
1m 5s  ◐  Lint / check             1m 3s
```

### Error Annotation Display (`internal/tui/view.go:80-114`)

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
    
    return b.String()
}
```

**Example Output**:

```text
1m 5s  ✗  Test Suite / unit tests  5m 30s
  ┃ src/parser_test.go:42 - undefined: TestData
  ┃ src/parser_test.go:57 - unused variable 'result'
```

**Conditional Display**: Only shown for checks with `conclusion: "failure"` or `"timed_out"`.

### Startup Phase Rendering (`internal/tui/view.go:179-199`)

```go
func (m Model) renderStartupPhase() string {
    sinceStart := time.Since(m.startTime)
    
    var b strings.Builder
    
    if sinceStart < 2*time.Minute {
        b.WriteString(fmt.Sprintf("%s ", m.spinner.View()))
        b.WriteString(m.styles.Running.Render(fmt.Sprintf("Startup Phase (%s elapsed):\n", timing.FormatDuration(sinceStart))))
        b.WriteString("  ⏳ Waiting for Actions to start...\n")
        b.WriteString("  💡 GitHub typically takes 30-90s to queue jobs after PR creation\n")
    } else if sinceStart < 3*time.Minute {
        b.WriteString(fmt.Sprintf("%s ", m.spinner.View()))
        b.WriteString(m.styles.Running.Render(fmt.Sprintf("Still waiting (%s elapsed)...\n", timing.FormatDuration(sinceStart))))
        b.WriteString("  ⏳ Checks may be delayed or not configured for this PR\n")
    } else {
        b.WriteString(m.styles.Queued.Render("No checks found.\n"))
        b.WriteString("  This PR may not have workflows configured, or they may have been skipped.\n")
    }
    
    return b.String()
}
```

**Phased Messaging**:

1. **0-2 minutes**: Helpful "Startup Phase" with spinner
2. **2-3 minutes**: "Still waiting" warning
3. **>3 minutes**: "No checks found" (likely no workflows)

**Why this matters**: GitHub Actions has significant startup delay (30-90s). Without this messaging, users might think the tool is frozen.

---

## 9. Error Handling & Edge Cases

### Network Errors During Polling (`internal/tui/update.go:67-70`)

```go
case ChecksUpdateMsg:
    if msg.Err != nil {
        m.err = msg.Err
        return m, nil  // Continue polling, don't quit
    }
```

**Design Decision**: Network errors are non-fatal. The TUI displays the error but continues polling. This allows recovery from transient network issues.

**User Experience**: Error appears in the UI, but the spinner keeps running and polling continues every 5s.

### Authentication Failures (`main.go:162-164`)

```go
token, err := ghclient.GetToken()
if err != nil {
    fmt.Fprintf(os.Stderr, "Failed to get GitHub token: %v\n", err)
    return 1
}
```

**Error Message**: "authentication failed: set GITHUB_TOKEN or run `gh auth login`"

**Exit Code**: 1 (failure)

**Why fail fast?** Without authentication, no API calls can succeed. Better to exit immediately with a clear error.

### PR Detection Failures (`main.go:143-149`)

```go
n, err := ghclient.GetCurrentPR()
if err != nil {
    fmt.Fprintf(os.Stderr, "Failed to detect PR: %v\n", err)
    fmt.Fprintf(os.Stderr, "Make sure you're on a PR branch or provide a PR number: gh-observer <number>\n")
    return 1
}
```

**Causes**:

- Not on a PR branch (local branch with no PR)
- `gh` CLI not installed
- Not authenticated with `gh auth login`

**Recovery**: Suggest providing explicit PR number.

### No Checks Found (`main.go:72-77` snapshot, `view.go:41-43` TUI)

**Snapshot mode**: Returns exit code 0, displays message that checks may still be starting.

**TUI mode**: Shows "Startup Phase" message with helpful guidance.

**Why treat as success?** Empty check list isn't a failure condition. The PR might:

- Be brand new (checks starting up)
- Have no workflows (legitimate configuration)
- Have skipped workflows (workflow conditions not met)

### Rate Limit Handling (`internal/tui/update.go:38-40`)

```go
if m.rateLimitRemaining < 10 {
    return m, tick(m.refreshInterval * 3)  // Back off to 15s
}
```

**Backoff Strategy**: When remaining API calls < 10, poll interval triples from 5s to 15s.

**Why 10?** Provides buffer to avoid hitting 0, which would cause errors.

**Rate Limit Display** (`view.go:66-69`):

```go
if m.rateLimitRemaining < 100 {
    b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
}
```

Shows warning when remaining calls < 100.

### GraphQL Query Failures (`internal/github/graphql.go:108-111`)

```go
err := client.Query(ctx, &query, variables)
if err != nil {
    return nil, 0, err
}
```

Returns empty check runs and 0 for rate limit, allowing the caller to handle the error.

### Missing Timestamps (`internal/timing/calculator.go`)

All timing functions check for nil timestamps:

```go
func QueueLatency(commitTime time.Time, check ghclient.CheckRunInfo) time.Duration {
    if check.StartedAt == nil || commitTime.IsZero() {
        return 0  // Can't calculate without timestamps
    }
    ...
}
```

**Graceful Degradation**: Returns 0 instead of panicking, displays "-" in the UI.

---

## 10. Data Flow Diagrams

### Snapshot Mode Flow

```text
main.go:113 run()
    │
    ├── config.Load()                               [internal/config/config.go:24]
    │   └── Returns: Config{RefreshInterval, Colors, EnableLinks}
    │
    ├── tui.NewStyles()                             [internal/tui/styles.go:23]
    │   └── Returns: Styles{Success, Failure, Running, Queued}
    │
    ├── Determine PR number
    │   ├── If argument provided: strconv.Atoi()
    │   └── Else: ghclient.GetCurrentPR()          [internal/github/pr.go:25]
    │       └── Runs: gh pr view --json number --jq .number
    │
    ├── ghclient.ParseOwnerRepo()                   [internal/github/pr.go:41]
    │   ├── Runs: git remote get-url origin
    │   └── Parses: git@github.com:owner/repo.git or https://github.com/owner/repo
    │
    ├── ghclient.GetToken()                         [internal/github/client.go:15]
    │   ├── Check: GITHUB_TOKEN environment variable
    │   └── Else: Run `gh auth token`
    │
    ├── Check terminal: term.IsTerminal()
    │   └── FALSE: Run snapshot mode
    │
    └── runSnapshot()                               [main.go:39]
        │
        ├── ghclient.NewClient()                    [internal/github/client.go:36]
        │   └── Creates REST API client with OAuth2
        │
        ├── ghclient.FetchPRInfo()                  [internal/github/pr.go:69]
        │   ├── REST: GET /repos/{owner}/{repo}/pulls/{prNumber}
        │   ├── REST: GET /repos/{owner}/{repo}/commits/{sha}
        │   └── Returns: PRInfo{Title, HeadSHA, HeadCommitDate}
        │
        ├── ghclient.FetchCheckRunsGraphQL()        [internal/github/graphql.go:94]
        │   ├── GraphQL: Query PR → Commits → StatusCheckRollup
        │   └── Returns: []CheckRunInfo{Name, WorkflowName, Status, ...}
        │
        ├── tui.CalculateColumnWidths()             [internal/tui/display.go:126]
        │   └── Returns: ColumnWidths{QueueWidth, NameWidth, DurationWidth}
        │
        └── Render output
            ├── tui.FormatHeaderColumns()           [display.go:186]
            ├── tui.BuildNameColumn()               [display.go:112]
            ├── tui.FormatQueueLatency()            [display.go:14]
            ├── tui.FormatDuration()                [display.go:30]
            └── Determine exit code
```

### TUI Mode Flow

```text
main.go:173 (TUI mode)
    │
    ├── tui.NewModel()                              [internal/tui/model.go:57]
    │   └── Returns: Model{ctx, token, owner, repo, prNumber, spinner, ...}
    │
    ├── tea.NewProgram(model)                       [Bubbletea framework]
    │   └── Creates program with model
    │
    └── p.Run()                                     [Blocking event loop]
        │
        └── model.Init()                            [internal/tui/update.go:13]
            │
            ├── Returns: tea.Batch(
            │       spinner.Tick,                   [Animate spinner]
            │       fetchPRInfo(),                  [Get PR metadata]
            │       tick(m.refreshInterval)         [Schedule poll timer]
            │   )
            │
            └── Message processing loop
                │
                ├── [PRInfoMsg received]
                │   ├── Store: prTitle, headSHA, headCommitTime
                │   └── Return: fetchCheckRuns()    [Fetch checks immediately]
                │
                ├── [TickMsg received]
                │   ├── Check: rateLimitRemaining < 10?
                │   │   └── YES: Back off to 15s interval
                │   └── Return: tea.Batch(
                │           fetchCheckRuns(),       [Poll for updates]
                │           tick(m.refreshInterval) [Schedule next poll]
                │       )
                │
                ├── [ChecksUpdateMsg received]
                │   ├── Store: checkRuns, rateLimitRemaining
                │   ├── Update: lastUpdate = time.Now()
                │   ├── Check: allChecksComplete()?
                │   │   ├── YES: determineExitCode()
                │   │   └──        Return: tea.Quit
                │   └── NO: Return: nil               [Continue polling]
                │
                ├── [spinner.TickMsg received]
                │   └── Update spinner animation
                │
                └── [tea.KeyMsg received]
                    └── If "q" or "ctrl+c": tea.Quit
```

### State Transitions

```text
Initial State (Model created)
    │
    ├─> Loading PR Info (Init() → fetchPRInfo())
    │       │
    │       ├─> [Success] → PRInfoMsg
    │       │       └─> Fetch checks immediately
    │       │
    │       └─> [Failure] → PRInfoMsg{Err: ...}
    │               └─> Display error and quit
    │
    ├─> Polling Loop (every 5s)
    │       │
    │       ├─> [Checks empty] → Display "Startup Phase"
    │       │
    │       ├─> [Checks exist] → Display check list
    │       │       │
    │       │       ├─> [All completed] → Determine exit code → Quit
    │       │       │
    │       │       └─> [Some pending] → Continue polling
    │       │
    │       └─> [Rate limit < 10] → Reduce frequency to 15s
    │
    └─> User Input
            │
            └─> [q or Ctrl+C] → Set quitting=true → tea.Quit
```

---

## 11. Exit Behavior

### Exit Codes

The application returns exit codes to communicate results to calling processes (e.g., CI pipelines):

```go
// main.go:184-188
if m, ok := finalModel.(tui.Model); ok {
    return m.ExitCode()
}
return 0  // Fallback if type assertion fails
```

**Exit Code Semantics**:

| Code | Meaning | Example |
| ---- | ------- | ------- |
| 0 | Success | All checks passed |
| 0 | No checks | PR has no workflows (snapshot mode) |
| 0 | Incomplete checks | Checks still running (snapshot mode) |
| 1 | Check failure | One or more checks failed |
| 1 | Authentication error | Missing GITHUB_TOKEN |
| 1 | Network error | Failed to fetch PR info (TUI mode initialization) |
| 1 | Invalid input | Bad PR number argument |

### Exit Code Determination for TUI Mode

```go
func determineExitCode(checks []ghclient.CheckRunInfo) int {
    for _, check := range checks {
        if check.Conclusion == "failure" ||
           check.Conclusion == "timed_out" ||
           check.Conclusion == "action_required" {
            return 1
        }
    }
    return 0
}
```

**Failure Conditions**:

- `failure`: Test failures, build errors, etc.
- `timed_out`: GitHub Actions timeout (default 6 hours)
- `action_required`: Waiting for manual approval (e.g., environment protection rules)

**Success Conditions**:

- `success`: All steps passed
- `cancelled`: User manually cancelled (treated as non-failure)
- `skipped`: Job skipped due to conditions (treated as non-failure)
- `neutral`: Check completed with neutral status (rare)

### Completion Check (`internal/tui/update.go:143-155`)

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

**Status Values**:

- `queued`: Job is waiting to run
- `in_progress`: Job is currently running
- `completed`: Job finished (check conclusion for result)

### Clean Shutdown

TUI mode exits cleanly by:

1. Setting `m.quitting = true` (prevents "Press q to quit" message)
2. Returning `tea.Quit` command (stops event loop)
3. Bubbletea restores terminal state
4. Final model passed back to `main()`
5. Exit code extracted from model
6. `os.Exit(exitCode)` terminates process

**Why not just `os.Exit()` directly?** Bubbletea needs to restore terminal settings (cooked mode, cursor visibility, etc.). The `tea.Quit` command ensures clean shutdown.

---

## Summary

gh-observer is a well-architected CLI application that demonstrates several best practices:

1. **Clean separation of concerns**: Distinct packages for config, GitHub API, timing calculations, and TUI rendering
2. **Efficient API usage**: GraphQL for complex queries, REST for simple metadata
3. **Graceful error handling**: Non-fatal errors during polling, fatal errors at initialization
4. **Terminal-aware output**: Snapshot mode for CI, TUI mode for interactive use
5. **Rate limit awareness**: Backoff strategy and remaining quota display
6. **User feedback**: Startup phase messaging, real-time updates, clear error messages

The codebase follows the Elm Architecture pattern through Bubbletea, making the state management predictable and testable. The linear execution flow from initialization through polling to exit is clear and well-structured.

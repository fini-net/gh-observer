package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fini-net/gh-observer/internal/config"
	"github.com/fini-net/gh-observer/internal/debug"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/fini-net/gh-observer/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var quickFlag bool
var debugFlag bool
var repoFlag string

func init() {
	rootCmd.Flags().BoolVarP(&quickFlag, "quick", "q", false, "Skip fetching historical average runtimes")
	rootCmd.Flags().BoolVarP(&debugFlag, "debug", "d", false, "Log suppressed errors and internal state to a file")
	rootCmd.Flags().StringVarP(&repoFlag, "repo", "r", "", "Repository (owner/repo or URL); implies persist mode")
}

var rootCmd = &cobra.Command{
	Use:   "gh-observer [PR_NUMBER | PR_URL | ACTIONS_RUN_URL]",
	Short: "Watch GitHub PR checks or Actions runs with runtime metrics",
	Long: `gh-observer is a GitHub PR check watcher CLI tool that improves on 
'gh pr checks --watch' by showing runtime metrics, queue latency, 
and better handling of startup delays.

Supports watching checks on external repositories by passing a full PR URL:
  gh-observer https://github.com/owner/repo/pull/123

Also supports watching GitHub Actions runs by passing a run URL:
  gh-observer https://github.com/owner/repo/actions/runs/123456789

Use --repo to specify a repository (enables persist mode for continuous watching):
  gh-observer --repo owner/repo 123      Watch PR #123 on owner/repo (persist)
  gh-observer --repo owner/repo          Auto-detect PR, persist mode
  gh-observer -r owner/repo 123          Short form`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		exitCode := run(args)
		os.Exit(exitCode)
	},
}

// runMode determines which mode the application is running in.
type runMode int

const (
	modePR        runMode = iota // Watch a PR's checks
	modeRun                       // Watch an Actions workflow run
	modeRepoWatch                 // Watch all workflow runs on a repo
)

// runArgs holds the parsed arguments for either mode.
type runArgs struct {
	mode       runMode
	owner      string
	repo       string
	prNumber   int
	runID      int64
	persist    bool
}

func run(args []string) int {
	ctx := context.Background()

	if debugFlag {
		if err := debug.Enable(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to enable debug logging: %v\n", err)
			return 1
		}
		defer debug.Close()
		fmt.Fprintf(os.Stderr, "Debug log: %s\n", debug.LogPath())
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		return 1
	}

	// Create styles
	styles := tui.NewStyles(
		cfg.Colors.Success,
		cfg.Colors.Failure,
		cfg.Colors.Running,
		cfg.Colors.Queued,
	)

	// Parse arguments
	parsed, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	// Get GitHub token
	token, err := ghclient.GetToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get GitHub token: %v\n", err)
		return 1
	}

	switch parsed.mode {
	case modePR:
		return runPRMode(ctx, token, parsed, cfg, styles)
	case modeRun:
		return runActionsMode(ctx, token, parsed, cfg, styles)
	case modeRepoWatch:
		return runRepoWatchMode(ctx, token, parsed, cfg, styles)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode\n")
		return 1
	}
}

// parseArgs determines whether the argument is a PR number, PR URL, or Actions run URL.
// When --repo is set, it extracts owner/repo from the flag and implies persist mode.
func parseArgs(args []string) (runArgs, error) {
	var flagOwner, flagRepo string
	persist := false

	if repoFlag != "" {
		owner, repo, err := ghclient.ParseRepoFlag(repoFlag)
		if err != nil {
			return runArgs{}, err
		}
		flagOwner = owner
		flagRepo = repo
		persist = true
	}

	if len(args) == 0 {
		if flagOwner != "" {
			prNumber, _, _, err := ghclient.GetCurrentPRWithRepo()
			if err != nil {
				// No PR detected — watch the repo's workflow runs
				return runArgs{mode: modeRepoWatch, owner: flagOwner, repo: flagRepo, persist: persist}, nil
			}
			// PR detected — use it with the flag's owner/repo
			return runArgs{mode: modePR, owner: flagOwner, repo: flagRepo, prNumber: prNumber, persist: persist}, nil
		}
		// Auto-detect PR from current branch
		prNumber, owner, repo, err := ghclient.GetCurrentPRWithRepo()
		if err != nil {
			if ghclient.IsJujutsu() {
				return runArgs{}, fmt.Errorf("failed to detect PR in jj (Jujutsu) repo: %v\n\nHint: In a jj repo, you may need to:\n  1. Pass an explicit PR number: gh-observer 123\n  2. Pass a PR URL: gh-observer https://github.com/owner/repo/pull/123\n  3. Enable colocated mode: jj git colocation enable", err)
			}
			return runArgs{}, fmt.Errorf("failed to detect PR: %v\nMake sure you're on a PR branch or provide a PR number or URL", err)
		}
		return runArgs{mode: modePR, owner: owner, repo: repo, prNumber: prNumber}, nil
	}

	arg := args[0]

	// Try PR URL first
	if owner, repo, prNumber, err := ghclient.ParsePRURL(arg); err == nil {
		if flagOwner != "" && (flagOwner != owner || flagRepo != repo) {
			return runArgs{}, fmt.Errorf("--repo %s/%s conflicts with PR URL owner/repo %s/%s", flagOwner, flagRepo, owner, repo)
		}
		if flagOwner != "" {
			owner = flagOwner
			repo = flagRepo
		}
		return runArgs{mode: modePR, owner: owner, repo: repo, prNumber: prNumber, persist: persist}, nil
	}

	// Try Actions run URL
	if owner, repo, runID, err := ghclient.ParseActionsRunURL(arg); err == nil {
		if flagOwner != "" && (flagOwner != owner || flagRepo != repo) {
			return runArgs{}, fmt.Errorf("--repo %s/%s conflicts with Actions run URL owner/repo %s/%s", flagOwner, flagRepo, owner, repo)
		}
		if flagOwner != "" {
			owner = flagOwner
			repo = flagRepo
		}
		return runArgs{mode: modeRun, owner: owner, repo: repo, runID: runID, persist: persist}, nil
	}

	// Try numeric PR number
	if n, convErr := strconv.Atoi(arg); convErr == nil {
		if flagOwner != "" {
			// Use --repo flag's owner/repo directly, skip gh pr view
			return runArgs{mode: modePR, owner: flagOwner, repo: flagRepo, prNumber: n, persist: persist}, nil
		}
		prNumber, owner, repo, err := ghclient.GetPRWithRepo(n)
		if err != nil {
			return runArgs{}, fmt.Errorf("failed to get PR #%d: %v", n, err)
		}
		return runArgs{mode: modePR, owner: owner, repo: repo, prNumber: prNumber}, nil
	}

	return runArgs{}, fmt.Errorf("invalid PR number, PR URL, or Actions run URL: %s", arg)
}

// runPRMode handles watching a PR's checks.
func runPRMode(ctx context.Context, token string, parsed runArgs, cfg *config.Config, styles tui.Styles) int {
	owner, repo, prNumber := parsed.owner, parsed.repo, parsed.prNumber

	// Check if running in a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runSnapshot(ctx, token, owner, repo, prNumber, cfg.EnableLinks, quickFlag)
	}

	// Create model
	model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag, parsed.persist, cfg.PersistRefreshInterval)

	// Run TUI
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		return 1
	}

	if m, ok := finalModel.(tui.Model); ok {
		return m.ExitCode()
	}

	return 0
}

// runActionsMode handles watching an Actions workflow run.
func runActionsMode(ctx context.Context, token string, parsed runArgs, cfg *config.Config, styles tui.Styles) int {
	owner, repo, runID := parsed.owner, parsed.repo, parsed.runID

	// Check if running in a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runRunSnapshot(ctx, owner, repo, runID, cfg.EnableLinks, quickFlag)
	}

	// Create run model
	model := tui.NewRunModel(ctx, token, owner, repo, runID, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag, parsed.persist, cfg.PersistRefreshInterval)

	// Run TUI
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		return 1
	}

	if m, ok := finalModel.(tui.RunModel); ok {
		return m.ExitCode()
	}

	return 0
}

// runRepoWatchMode handles watching a repository's workflow runs.
func runRepoWatchMode(ctx context.Context, token string, parsed runArgs, cfg *config.Config, styles tui.Styles) int {
	owner, repo := parsed.owner, parsed.repo

	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runRepoWatchSnapshot(ctx, token, owner, repo, cfg.EnableLinks)
	}

	model := tui.NewRepoWatchModel(ctx, token, owner, repo, cfg.RefreshInterval, styles, cfg.EnableLinks, parsed.persist, cfg.PersistRefreshInterval)

	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		return 1
	}

	if m, ok := finalModel.(tui.RepoWatchModel); ok {
		return m.ExitCode()
	}

	return 0
}

// runSnapshot prints a one-time snapshot of PR check status (non-interactive mode)
func runSnapshot(ctx context.Context, token, owner, repo string, prNumber int, enableLinks bool, quick bool) int {
	client, err := ghclient.NewClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create GitHub client: %v\n", err)
		return 1
	}

	prInfo, err := ghclient.FetchPRInfo(ctx, client, owner, repo, prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch PR info: %v\n", err)
		return 1
	}

	headCommitTime, err := time.Parse(time.RFC3339, prInfo.HeadCommitDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse commit time: %v\n", err)
		return 1
	}

	checkRuns, _, err := ghclient.FetchCheckRunsGraphQL(ctx, token, owner, repo, prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch check runs: %v\n", err)
		return 1
	}

	fmt.Printf("PR #%d: %s\n\n", prNumber, prInfo.Title)

	if len(checkRuns) == 0 {
		sinceCreation := time.Since(headCommitTime)
		fmt.Printf("No checks found (commit pushed %s ago)\n", timing.FormatDuration(sinceCreation))
		fmt.Println("Checks may still be starting up or not configured for this PR")
		return 0
	}

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

	widths := tui.CalculateColumnWidths(checkRuns, headCommitTime, jobAverages)

	headerQueue, headerName, headerDuration, headerAvg := tui.FormatHeaderColumns(widths)
	fmt.Printf("%s   %s  %s  %s\n\n", headerQueue, headerName, headerDuration, headerAvg)

	exitCode := 0
	for _, check := range checkRuns {
		nameCol := tui.BuildNameColumn(check, widths, enableLinks)
		queueText := tui.FormatQueueLatency(check, headCommitTime)
		durationText := tui.FormatDuration(check)
		avgText := tui.FormatAvg(check, jobAverages)
		icon := tui.GetCheckIcon(check.Status, check.Conclusion)

		queueCol, _, durationCol, avgCol := tui.FormatAlignedColumns(queueText, tui.FormatCheckNameWithTruncate(check, widths.NameWidth), durationText, avgText, widths)

		fmt.Printf("%s %s %s  %s  %s\n", queueCol, icon, nameCol, durationCol, avgCol)

		if check.Status == "completed" {
			if ghclient.FailureConclusion(check.Conclusion) {
				exitCode = 1
			}
		}
	}

	return exitCode
}

// runRunSnapshot prints a one-time snapshot of Actions run status (non-interactive mode)
func runRunSnapshot(ctx context.Context, owner, repo string, runID int64, enableLinks bool, quick bool) int {
	token, err := ghclient.GetToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get GitHub token: %v\n", err)
		return 1
	}
	client := ghclient.NewClientFromToken(ctx, token)

	runInfo, err := ghclient.FetchRunInfo(ctx, client, owner, repo, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch run info: %v\n", err)
		return 1
	}

	jobs, _, err := ghclient.FetchRunJobs(ctx, client, owner, repo, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch jobs: %v\n", err)
		return 1
	}

	var timeSinceStr string
	if runInfo.HeadCommitTime != nil && !runInfo.HeadCommitTime.IsZero() {
		timeSinceStr = fmt.Sprintf("Pushed %s ago", timing.FormatDuration(time.Since(runInfo.HeadCommitTime.Time)))
	} else if runInfo.CreatedAt != nil && !runInfo.CreatedAt.IsZero() {
		timeSinceStr = fmt.Sprintf("Created %s ago", timing.FormatDuration(time.Since(runInfo.CreatedAt.Time)))
	}

	fmt.Printf("%s/%s: %s\n", owner, repo, runInfo.DisplayTitle)
	if timeSinceStr != "" {
		fmt.Println(timeSinceStr)
	}
	fmt.Println()

	if len(jobs) == 0 {
		fmt.Println("No jobs found")
		return 0
	}

	var jobAverages map[string]time.Duration
	if !quick {
		checkRuns := ghclient.WorkflowJobInfoToCheckRuns(jobs)
		avgs, _, _, err := ghclient.FetchJobAverages(ctx, client, owner, repo, checkRuns, nil, nil)
		if err == nil {
			jobAverages = avgs
		}
	}

	widths := tui.CalculateRunColumnWidths(jobs, jobAverages)

	headerName, headerDuration, headerAvg := tui.FormatRunHeaderColumns(widths)
	fmt.Printf("  %s  %s  %s\n\n", headerName, headerDuration, headerAvg)

	exitCode := 0
	for _, job := range jobs {
		nameCol := tui.BuildRunJobNameColumn(job, widths, enableLinks)
		durationText := tui.FormatRunJobDuration(job)
		avgText := tui.FormatRunJobAvg(job, jobAverages)
		icon := tui.GetCheckIcon(job.Status, job.Conclusion)

		fmt.Printf("%s %s  %s  %s\n", icon, nameCol, durationText, avgText)

		if job.Status == "completed" {
			if ghclient.FailureJobConclusion(job.Conclusion) {
				exitCode = 1
			}
		}
	}

	return exitCode
}

// runRepoWatchSnapshot prints a one-time snapshot of workflow runs (non-interactive mode)
func runRepoWatchSnapshot(ctx context.Context, token, owner, repo string, enableLinks bool) int {
	client, err := ghclient.NewClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create GitHub client: %v\n", err)
		return 1
	}

	runs, _, err := ghclient.FetchRepositoryRuns(ctx, client, owner, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch repository runs: %v\n", err)
		return 1
	}

	fmt.Printf("%s/%s — workflow runs\n\n", owner, repo)

	if len(runs) == 0 {
		fmt.Println("No workflow runs found")
		return 0
	}

	fmt.Printf("  %-40s  %-10s  %-10s  %s\n", "Workflow", "Status", "Duration", "Branch")
	fmt.Println()

	exitCode := 0
	for _, run := range runs {
		name := run.DisplayTitle
		if len(name) > 40 {
			name = name[:37] + "..."
		}

		status := run.Status
		if status == "completed" {
			status = run.Conclusion
			if status == "" {
				status = "completed"
			}
		}

		var duration string
		if run.RunStartedAt != nil && !run.RunStartedAt.IsZero() {
			if run.Status == "completed" && run.UpdatedAt != nil && !run.UpdatedAt.IsZero() {
				duration = timing.FormatDuration(run.UpdatedAt.Time.Sub(run.RunStartedAt.Time))
			} else {
				duration = timing.FormatDuration(time.Since(run.RunStartedAt.Time))
			}
		} else if run.CreatedAt != nil && !run.CreatedAt.IsZero() {
			duration = timing.FormatDuration(time.Since(run.CreatedAt.Time))
		}

		branch := run.HeadBranch
		if branch == "" {
			branch = "-"
		}

		icon := tui.GetCheckIcon(run.Status, run.Conclusion)
		fmt.Printf("%s %-40s  %-10s  %-10s  %s\n", icon, name, status, duration, branch)

		if run.Status == "completed" {
			if ghclient.FailureRunConclusion(run.Conclusion) {
				exitCode = 1
			}
		}
	}

	return exitCode
}
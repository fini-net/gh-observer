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

// runSnapshot prints a one-time snapshot of PR check status (non-interactive mode)
func runSnapshot(ctx context.Context, token, owner, repo string, prNumber int, enableLinks bool, quick bool) int {
	// Create GitHub client for PR info
	client, err := ghclient.NewClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create GitHub client: %v\n", err)
		return 1
	}

	// Fetch PR info
	prInfo, err := ghclient.FetchPRInfo(ctx, client, owner, repo, prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch PR info: %v\n", err)
		return 1
	}

	// Parse head commit time
	headCommitTime, err := time.Parse(time.RFC3339, prInfo.HeadCommitDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse commit time: %v\n", err)
		return 1
	}

	// Fetch check runs
	checkRuns, _, err := ghclient.FetchCheckRunsGraphQL(ctx, token, owner, repo, prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch check runs: %v\n", err)
		return 1
	}

	// Print header
	fmt.Printf("PR #%d: %s\n\n", prNumber, prInfo.Title)

	// Handle case where no checks exist yet
	if len(checkRuns) == 0 {
		sinceCreation := time.Since(headCommitTime)
		fmt.Printf("No checks found (commit pushed %s ago)\n", timing.FormatDuration(sinceCreation))
		fmt.Println("Checks may still be starting up or not configured for this PR")
		return 0
	}

	// Fetch historical averages unless --quick was passed
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

	// Calculate column widths
	widths := tui.CalculateColumnWidths(checkRuns, headCommitTime, jobAverages)

	// Print column headers
	headerQueue, headerName, headerDuration, headerAvg := tui.FormatHeaderColumns(widths)
	fmt.Printf("%s   %s  %s  %s\n\n", headerQueue, headerName, headerDuration, headerAvg)

	// Print each check
	exitCode := 0
	for _, check := range checkRuns {
		// Build name column with correct padding outside any hyperlink
		nameCol := tui.BuildNameColumn(check, widths, enableLinks)
		queueText := tui.FormatQueueLatency(check, headCommitTime)
		durationText := tui.FormatDuration(check)
		avgText := tui.FormatAvg(check, jobAverages)
		icon := tui.GetCheckIcon(check.Status, check.Conclusion)

		// Compute queue, duration, and avg columns; discard name since BuildNameColumn owns it
		queueCol, _, durationCol, avgCol := tui.FormatAlignedColumns(queueText, tui.FormatCheckNameWithTruncate(check, widths.NameWidth), durationText, avgText, widths)

		// Print line without colors (plain text for non-terminal)
		fmt.Printf("%s %s %s  %s  %s\n", queueCol, icon, nameCol, durationCol, avgCol)

		// Determine exit code based on conclusions
		if check.Status == "completed" {
			if ghclient.FailureConclusion(check.Conclusion) {
				exitCode = 1
			}
		}
	}

	return exitCode
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
	var owner, repo string
	var prNumber int

	if len(args) > 0 {
		arg := args[0]
		if owner, repo, prNumber, err = ghclient.ParsePRURL(arg); err == nil {
			// valid PR URL
		} else if n, convErr := strconv.Atoi(arg); convErr == nil {
			// numeric PR number
			prNumber, owner, repo, err = ghclient.GetPRWithRepo(n)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get PR #%d: %v\n", n, err)
				return 1
			}
		} else {
			fmt.Fprintf(os.Stderr, "Invalid PR number or URL: %s\n", arg)
			return 1
		}
	} else {
		// Auto-detect PR from current branch (correctly handles forks)
		prNumber, owner, repo, err = ghclient.GetCurrentPRWithRepo()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to detect PR: %v\n", err)
			fmt.Fprintf(os.Stderr, "Make sure you're on a PR branch or provide a PR number or URL\n")
			return 1
		}
	}

	// Get GitHub token
	token, err := ghclient.GetToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get GitHub token: %v\n", err)
		return 1
	}

	// Check if running in a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		// Non-interactive mode: print snapshot and exit
		return runSnapshot(ctx, token, owner, repo, prNumber, cfg.EnableLinks, quickFlag)
	}

	// Create model
	model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles, cfg.EnableLinks, quickFlag)

	// Run TUI (keeps output visible after exit)
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		return 1
	}

	// Extract exit code from final model
	if m, ok := finalModel.(tui.Model); ok {
		return m.ExitCode()
	}

	return 0
}

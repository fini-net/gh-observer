package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fini-net/gh-observer/internal/config"
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

var rootCmd = &cobra.Command{
	Use:   "gh-observer [PR_NUMBER]",
	Short: "Watch GitHub PR checks with runtime metrics",
	Long: `gh-observer is a GitHub PR check watcher CLI tool that improves on 
'gh pr checks --watch' by showing runtime metrics, queue latency, 
and better handling of startup delays.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		exitCode := run(args)
		os.Exit(exitCode)
	},
}

// runSnapshot prints a one-time snapshot of PR check status (non-interactive mode)
func runSnapshot(ctx context.Context, token, owner, repo string, prNumber int) int {
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

	// Calculate column widths
	widths := tui.CalculateColumnWidths(checkRuns, headCommitTime)

	// Print column headers
	headerQueue, headerName, headerDuration := tui.FormatHeaderColumns(widths)
	fmt.Printf("%s   %s  %s\n\n", headerQueue, headerName, headerDuration)

	// Print each check
	exitCode := 0
	for _, check := range checkRuns {
		// Format check data
		name := tui.FormatCheckNameWithTruncate(check, widths.NameWidth)
		queueText := tui.FormatQueueLatency(check, headCommitTime)
		durationText := tui.FormatDuration(check)
		icon := tui.GetCheckIcon(check.Status, check.Conclusion)

		// Format columns
		queueCol, _, durationCol := tui.FormatAlignedColumns(queueText, name, durationText, widths)

		// Make name clickable with OSC 8 hyperlink
		nameWithLink := tui.FormatLink(name, check.DetailsURL)

		// Print line without colors (plain text for non-terminal)
		fmt.Printf("%s %s %s  %s\n", queueCol, icon, nameWithLink, durationCol)

		// Determine exit code based on conclusions
		if check.Status == "completed" {
			conclusion := check.Conclusion
			if conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required" {
				exitCode = 1
			}
		}
	}

	return exitCode
}

func run(args []string) int {
	ctx := context.Background()

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
	var prNumber int
	if len(args) > 0 {
		// PR number provided as argument
		n, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid PR number: %s\n", args[0])
			return 1
		}
		prNumber = n
	} else {
		// Auto-detect PR from current branch
		n, err := ghclient.GetCurrentPR()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to detect PR: %v\n", err)
			fmt.Fprintf(os.Stderr, "Make sure you're on a PR branch or provide a PR number: gh-observer <number>\n")
			return 1
		}
		prNumber = n
	}

	// Get owner and repo
	owner, repo, err := ghclient.ParseOwnerRepo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse repository: %v\n", err)
		return 1
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
		return runSnapshot(ctx, token, owner, repo, prNumber)
	}

	// Create model
	model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles)

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

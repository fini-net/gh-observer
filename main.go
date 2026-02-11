package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fini-net/gh-observer/internal/config"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/fini-net/gh-observer/internal/tui"
	"golang.org/x/term"
)

func main() {
	exitCode := run()
	os.Exit(exitCode)
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
	widths := calculateSnapshotWidths(checkRuns, headCommitTime)

	// Print column headers
	queuePad := widths.queueWidth - 7
	if queuePad < 0 {
		queuePad = 0
	}
	headerQueue := strings.Repeat(" ", queuePad) + "Startup"

	// Left-align "Workflow/Job" (12 chars)
	namePad := widths.nameWidth - 12
	if namePad < 0 {
		namePad = 0
	}
	headerName := "Workflow/Job" + strings.Repeat(" ", namePad)

	durationPad := widths.durationWidth - 8
	if durationPad < 0 {
		durationPad = 0
	}
	headerDuration := strings.Repeat(" ", durationPad) + "Duration"

	fmt.Printf("%s   %s  %s\n\n", headerQueue, headerName, headerDuration)

	// Print each check
	exitCode := 0
	for _, check := range checkRuns {
		printCheckRun(check, headCommitTime, widths)

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

// snapshotWidths stores calculated column widths for snapshot mode
type snapshotWidths struct {
	queueWidth    int
	nameWidth     int
	durationWidth int
}

// calculateSnapshotWidths determines column widths based on check run data
func calculateSnapshotWidths(checkRuns []ghclient.CheckRunInfo, headCommitTime time.Time) snapshotWidths {
	const (
		minNameWidth = 20
		maxNameWidth = 60
		minTimeWidth = 5
	)

	widths := snapshotWidths{
		queueWidth:    minTimeWidth,
		nameWidth:     minNameWidth,
		durationWidth: minTimeWidth,
	}

	for _, check := range checkRuns {
		// Measure queue latency text
		queueText := formatSnapshotQueueLatency(check, headCommitTime)
		if len(queueText) > widths.queueWidth {
			widths.queueWidth = len(queueText)
		}

		// Measure name (Workflow / Job format)
		name := check.Name
		if check.WorkflowName != "" {
			name = fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
		}
		nameLen := len(name)
		if nameLen > widths.nameWidth && nameLen <= maxNameWidth {
			widths.nameWidth = nameLen
		} else if nameLen > maxNameWidth {
			widths.nameWidth = maxNameWidth
		}

		// Measure duration text
		durationText := formatSnapshotDuration(check)
		if len(durationText) > widths.durationWidth {
			widths.durationWidth = len(durationText)
		}
	}

	return widths
}

// formatSnapshotQueueLatency formats queue time for a check
func formatSnapshotQueueLatency(check ghclient.CheckRunInfo, headCommitTime time.Time) string {
	if check.Status == "queued" {
		if !headCommitTime.IsZero() {
			return timing.FormatDuration(time.Since(headCommitTime))
		}
		return "-"
	}

	queueLatency := timing.QueueLatency(headCommitTime, check)
	if queueLatency > 0 {
		return timing.FormatDuration(queueLatency)
	}
	return "-"
}

// formatSnapshotDuration formats duration/runtime for a check
func formatSnapshotDuration(check ghclient.CheckRunInfo) string {
	switch check.Status {
	case "completed":
		duration := timing.FinalDuration(check)
		if duration > 0 {
			return timing.FormatDuration(duration)
		}
		return "-"
	case "in_progress":
		runtime := timing.Runtime(check)
		if runtime > 0 {
			return timing.FormatDuration(runtime)
		}
		return "-"
	default:
		return "-"
	}
}

// printCheckRun prints a single check run with aligned columns
func printCheckRun(check ghclient.CheckRunInfo, headCommitTime time.Time, widths snapshotWidths) {
	status := check.Status
	conclusion := check.Conclusion

	// Format name as "Workflow / Job" or just "Job"
	name := check.Name
	if check.WorkflowName != "" {
		name = fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
	}

	// Truncate name if needed
	if len(name) > widths.nameWidth {
		name = name[:widths.nameWidth-1] + "…"
	}

	// Get column data
	queueText := formatSnapshotQueueLatency(check, headCommitTime)
	durationText := formatSnapshotDuration(check)

	// Determine icon
	var icon string
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			icon = "✓"
		case "failure":
			icon = "✗"
		case "cancelled":
			icon = "⊗"
		case "skipped":
			icon = "⊘"
		case "timed_out":
			icon = "⏱"
		case "action_required":
			icon = "!"
		default:
			icon = "?"
		}
	case "in_progress":
		icon = "◐"
	case "queued":
		icon = "⏸"
	default:
		icon = "?"
	}

	// Build columns with padding
	queuePadding := widths.queueWidth - len(queueText)
	if queuePadding < 0 {
		queuePadding = 0
	}
	queueCol := strings.Repeat(" ", queuePadding) + queueText

	namePadding := widths.nameWidth - len(name)
	if namePadding < 0 {
		namePadding = 0
	}
	nameCol := name + strings.Repeat(" ", namePadding)

	durationPadding := widths.durationWidth - len(durationText)
	if durationPadding < 0 {
		durationPadding = 0
	}
	durationCol := strings.Repeat(" ", durationPadding) + durationText

	// Print line without colors (plain text for non-terminal) [queue][1 space][icon][1 space][name][2 spaces][duration][newline]
	fmt.Printf("%s %s %s  %s\n", queueCol, icon, nameCol, durationCol)
}

func run() int {
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
	if len(os.Args) > 1 {
		// PR number provided as argument
		n, err := strconv.Atoi(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid PR number: %s\n", os.Args[1])
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

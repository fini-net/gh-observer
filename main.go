package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fini-net/gh-observer/internal/config"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/tui"
)

func main() {
	exitCode := run()
	os.Exit(exitCode)
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

	// Create model
	model := tui.NewModel(ctx, token, owner, repo, prNumber, cfg.RefreshInterval, styles)

	// Run TUI
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

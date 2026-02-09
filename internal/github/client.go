package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v58/github"
	"golang.org/x/oauth2"
)

// NewClient creates a GitHub API client using GITHUB_TOKEN env var or gh CLI
func NewClient(ctx context.Context) (*github.Client, error) {
	// Try GITHUB_TOKEN env var first
	token := os.Getenv("GITHUB_TOKEN")

	// Fall back to gh CLI
	if token == "" {
		cmd := exec.Command("gh", "auth", "token")
		output, err := cmd.Output()
		if err == nil {
			token = strings.TrimSpace(string(output))
		}
	}

	if token == "" {
		return nil, fmt.Errorf("authentication failed: set GITHUB_TOKEN or run `gh auth login`")
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc), nil
}

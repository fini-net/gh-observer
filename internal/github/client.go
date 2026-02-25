package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v83/github"
	"golang.org/x/oauth2"
)

// GetToken retrieves the GitHub token from GITHUB_TOKEN env var or gh CLI
func GetToken() (string, error) {
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
		return "", fmt.Errorf("authentication failed: set GITHUB_TOKEN or run `gh auth login`")
	}

	return token, nil
}

// NewClient creates a GitHub API client using GITHUB_TOKEN env var or gh CLI
func NewClient(ctx context.Context) (*github.Client, error) {
	token, err := GetToken()
	if err != nil {
		return nil, err
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc), nil
}

package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v89/github"
)

// GetToken retrieves the GitHub token from GITHUB_TOKEN env var or gh CLI
func GetToken() (string, error) {
	// Try GITHUB_TOKEN env var first
	token := os.Getenv("GITHUB_TOKEN")

	// Fall back to gh CLI
	if token == "" {
		cmd := exec.Command("gh", "auth", "token")
		output, err := cmd.CombinedOutput()
		if err == nil {
			token = strings.TrimSpace(string(output))
		} else {
			debug.Log("gh auth token failed", "err", err, "output", strings.TrimSpace(string(output)))
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
	return NewClientFromToken(token)
}

// NewClientFromToken creates a GitHub API client using an already-obtained token.
func NewClientFromToken(token string) (*github.Client, error) {
	return github.NewClient(github.WithAuthToken(token))
}

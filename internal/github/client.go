package github

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v90/github"
	"github.com/shurcooL/githubv4"
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

// safeGraphQLInt converts an architecture-dependent int to githubv4.Int
// (backed by int32) with a bounds check, preventing silent truncation on
// platforms where int is 64-bit. Returns an error if the value is out of
// int32 range. Resolves CodeQL "Incorrect conversion between integer types"
// finding on githubv4.Int(prNumber) conversions.
func safeGraphQLInt(n int) (githubv4.Int, error) {
	if n > math.MaxInt32 || n < math.MinInt32 {
		return 0, fmt.Errorf("value %d exceeds int32 range for GraphQL Int scalar", n)
	}
	return githubv4.Int(n), nil
}

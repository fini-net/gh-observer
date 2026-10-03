package github

import (
	"context"

	"github.com/google/go-github/v92/github"
)

// CheckRunsResult contains check runs and rate limit info
type CheckRunsResult struct {
	CheckRuns          []*github.CheckRun
	RateLimitRemaining int
}

// FetchCheckRuns retrieves check runs for a given commit SHA
func FetchCheckRuns(ctx context.Context, client *github.Client, owner, repo, sha string) (*CheckRunsResult, error) {
	opts := &github.ListCheckRunsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	result, resp, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, opts)
	if err != nil {
		return nil, err
	}

	// Extract rate limit from response. normalizeRateLimit maps a
	// zero-value rate (enterprise host with rate limiting disabled, or a
	// nil response) to the unknown default instead of a spurious
	// "0 remaining" (see normalizeRateLimit).
	remaining := unknownRateLimit
	if resp != nil {
		remaining = normalizeRateLimit(resp.Rate.Limit, resp.Rate.Remaining)
	}

	return &CheckRunsResult{
		CheckRuns:          result.CheckRuns,
		RateLimitRemaining: remaining,
	}, nil
}

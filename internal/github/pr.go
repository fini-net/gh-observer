package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/google/go-github/v89/github"
)

var (
	prURLPattern          = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)$`)
	actionsRunURLPattern = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/actions/runs/(\d+)$`)
)

// PRInfo contains metadata about a pull request
type PRInfo struct {
	Number         int
	Title          string
	HeadSHA        string
	CreatedAt      string
	HeadCommitDate string
}

// parsePRViewWithRepo parses JSON output from 'gh pr view --json number,url'
func parsePRViewWithRepo(jsonOutput []byte) (int, string, string, error) {
	var result struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}

	if err := json.Unmarshal(jsonOutput, &result); err != nil {
		return 0, "", "", fmt.Errorf("failed to parse PR info: %w", err)
	}

	if result.Number == 0 {
		return 0, "", "", fmt.Errorf("PR number is zero or missing")
	}

	if result.URL == "" {
		return 0, "", "", fmt.Errorf("PR URL is missing")
	}

	// Parse owner/repo from URL like https://github.com/owner/repo/pull/123
	owner, repo, prNum, err := ParsePRURL(result.URL)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to parse PR URL: %w", err)
	}

	// Sanity check: parsed PR number should match
	if prNum != result.Number {
		return 0, "", "", fmt.Errorf("PR number mismatch: URL has %d, JSON has %d", prNum, result.Number)
	}

	return result.Number, owner, repo, nil
}

// GetCurrentPRWithRepo auto-detects PR number and repository from current branch.
// This correctly handles forked repos by getting owner/repo from the PR URL
// rather than from the local git remote. In jj (Jujutsu) repos, sets GIT_DIR
// so that gh pr view can locate the git repository.
func GetCurrentPRWithRepo() (int, string, string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,url")
	SetGITDirForJJ(cmd)
	output, err := cmd.Output()
	if err != nil {
		return 0, "", "", fmt.Errorf("not on a PR branch or gh CLI not available")
	}

	return parsePRViewWithRepo(output)
}

// GetPRWithRepo fetches PR number and repository for an explicit PR number.
// This correctly handles forked repos by getting owner/repo from the PR URL.
// In jj (Jujutsu) repos, sets GIT_DIR so that gh pr view can locate the git repository.
func GetPRWithRepo(prNumber int) (int, string, string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--json", "number,url")
	SetGITDirForJJ(cmd)
	output, err := cmd.Output()
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to view PR #%d: %w", prNumber, err)
	}

	return parsePRViewWithRepo(output)
}

// ParseActionsRunURL extracts owner, repo, and run ID from a GitHub Actions run URL.
// Expected format: https://github.com/owner/repo/actions/runs/NNN
func ParseActionsRunURL(url string) (owner, repo string, runID int64, err error) {
	matches := actionsRunURLPattern.FindStringSubmatch(url)
	if len(matches) != 4 {
		return "", "", 0, fmt.Errorf("invalid Actions run URL: %s (expected https://github.com/owner/repo/actions/runs/NNN)", url)
	}
	id, err := strconv.ParseInt(matches[3], 10, 64)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid run ID: %w", err)
	}
	return matches[1], matches[2], id, nil
}

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL
func ParsePRURL(prURL string) (owner, repo string, prNumber int, err error) {
	matches := prURLPattern.FindStringSubmatch(prURL)
	if len(matches) != 4 {
		return "", "", 0, fmt.Errorf("invalid PR URL: %s (expected https://github.com/owner/repo/pull/NNN)", prURL)
	}
	prNum, err := strconv.Atoi(matches[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR number: %w", err)
	}
	return matches[1], matches[2], prNum, nil
}

// FetchPRInfo retrieves metadata about a pull request
func FetchPRInfo(ctx context.Context, client *github.Client, owner, repo string, prNumber int) (*PRInfo, error) {
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR #%d: %w", prNumber, err)
	}

	headSHA := pr.GetHead().GetSHA()

	// Fetch the commit to get its timestamp
	commit, _, err := client.Repositories.GetCommit(ctx, owner, repo, headSHA, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch commit %s: %w", headSHA, err)
	}

	commitDate := ""
	if !commit.GetCommit().GetCommitter().GetDate().IsZero() {
		commitDate = commit.GetCommit().GetCommitter().GetDate().Format(TimestampFormat)
	}

	return &PRInfo{
		Number:         prNumber,
		Title:          pr.GetTitle(),
		HeadSHA:        headSHA,
		CreatedAt:      pr.GetCreatedAt().Format(TimestampFormat),
		HeadCommitDate: commitDate,
	}, nil
}

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v84/github"
)

// PRInfo contains metadata about a pull request
type PRInfo struct {
	Number         int
	Title          string
	HeadSHA        string
	CreatedAt      string
	HeadCommitDate string
}

// GetCurrentPR auto-detects the PR number from the current branch using gh CLI
func GetCurrentPR() (int, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number", "--jq", ".number")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("not on a PR branch or gh CLI not available")
	}

	prNumber, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("invalid PR number: %w", err)
	}

	return prNumber, nil
}

// PRWithRepo contains PR number and its repository coordinates
type PRWithRepo struct {
	Number int
	Owner  string
	Repo   string
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
// rather than from the local git remote.
func GetCurrentPRWithRepo() (int, string, string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,url")
	output, err := cmd.Output()
	if err != nil {
		return 0, "", "", fmt.Errorf("not on a PR branch or gh CLI not available")
	}

	return parsePRViewWithRepo(output)
}

// GetPRWithRepo fetches PR number and repository for an explicit PR number.
// This correctly handles forked repos by getting owner/repo from the PR URL.
func GetPRWithRepo(prNumber int) (int, string, string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--json", "number,url")
	output, err := cmd.Output()
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to view PR #%d: %w", prNumber, err)
	}

	return parsePRViewWithRepo(output)
}

// ParseOwnerRepo extracts owner and repo from git remote origin
func ParseOwnerRepo() (string, string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to get git remote: %w", err)
	}

	return parseOwnerRepoFromURL(strings.TrimSpace(string(output)))
}

// parseOwnerRepoFromURL extracts owner and repo from a remote URL string
func parseOwnerRepoFromURL(url string) (string, string, error) {
	// Parse SSH format: git@github.com:owner/repo.git
	sshPattern := regexp.MustCompile(`git@github\.com:([^/]+)/(.+?)(?:\.git)?/?$`)
	if matches := sshPattern.FindStringSubmatch(url); len(matches) == 3 {
		return matches[1], strings.TrimSuffix(matches[2], "/"), nil
	}

	// Parse HTTPS format: https://github.com/owner/repo or https://github.com/owner/repo.git
	httpsPattern := regexp.MustCompile(`https://github\.com/([^/]+)/(.+?)(?:\.git)?/?$`)
	if matches := httpsPattern.FindStringSubmatch(url); len(matches) == 3 {
		return matches[1], strings.TrimSuffix(matches[2], "/"), nil
	}

	return "", "", fmt.Errorf("unable to parse owner/repo from remote URL: %s", url)
}

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL
func ParsePRURL(prURL string) (owner, repo string, prNumber int, err error) {
	pattern := regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)$`)
	matches := pattern.FindStringSubmatch(prURL)
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
		commitDate = commit.GetCommit().GetCommitter().GetDate().Format("2006-01-02T15:04:05Z")
	}

	return &PRInfo{
		Number:         prNumber,
		Title:          pr.GetTitle(),
		HeadSHA:        headSHA,
		CreatedAt:      pr.GetCreatedAt().Format("2006-01-02T15:04:05Z"),
		HeadCommitDate: commitDate,
	}, nil
}

// GetCurrentPRFull uses gh CLI to get full PR info including branch
func GetCurrentPRFull() (*PRInfo, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,title,headRefOid,createdAt,commits")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("not on a PR branch or gh CLI not available")
	}

	var result struct {
		Number     int    `json:"number"`
		Title      string `json:"title"`
		HeadRefOid string `json:"headRefOid"`
		CreatedAt  string `json:"createdAt"`
		Commits    []struct {
			Oid           string `json:"oid"`
			CommittedDate string `json:"committedDate"`
		} `json:"commits"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse PR info: %w", err)
	}

	// Find the head commit date
	headCommitDate := ""
	for _, commit := range result.Commits {
		if commit.Oid == result.HeadRefOid {
			headCommitDate = commit.CommittedDate
			break
		}
	}

	return &PRInfo{
		Number:         result.Number,
		Title:          result.Title,
		HeadSHA:        result.HeadRefOid,
		CreatedAt:      result.CreatedAt,
		HeadCommitDate: headCommitDate,
	}, nil
}

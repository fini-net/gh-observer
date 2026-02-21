package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v58/github"
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
	if commit.GetCommit().GetCommitter().GetDate().Time.IsZero() == false {
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

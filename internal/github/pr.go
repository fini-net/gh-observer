package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/go-github/v92/github"
)

// URL patterns for PR and Actions run links. Owner/repo segments are
// restricted to GitHub slug characters ([a-zA-Z0-9_.-]); looser matching let
// fuzzing find inputs like "https://github.com/ow ner/re po/pull/1" parse
// with spaces in owner/repo (found by FuzzParsePRURL). The host segment
// accepts any hostname (GitHub Enterprise support, issue #479): hosts are
// captured and lowercased, and unknown hosts fail later at the auth/API
// layer rather than being rejected here. Hosts must start and end with an
// alphanumeric; hosts with ports are not matched (documented limitation).
var (
	prURLPattern         = regexp.MustCompile(`^https?://([a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?)/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)/pull/(\d+)$`)
	actionsRunURLPattern = regexp.MustCompile(`^https?://([a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?)/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)/actions/runs/(\d+)$`)
)

// PRInfo contains metadata about a pull request. Only PR-level fields
// (number, title, head SHA, created-at) come from REST; the head commit's
// push time is sourced separately from the GraphQL check-runs query
// (FetchCheckRunsGraphQL), which fetches pushedDate in the same round-trip
// as the StatusCheckRollup. The REST commit endpoint only exposes
// committer/author timestamps, not pushedDate, so it is not used here.
type PRInfo struct {
	Number    int
	Title     string
	HeadSHA   string
	CreatedAt string
}

// parsePRViewWithRepo parses JSON output from 'gh pr view --json number,url'.
// The host is extracted from the PR URL, so watching a PR from inside an
// enterprise checkout resolves against the enterprise instance.
func parsePRViewWithRepo(jsonOutput []byte) (int, string, string, string, error) {
	var result struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}

	if err := json.Unmarshal(jsonOutput, &result); err != nil {
		return 0, "", "", "", fmt.Errorf("failed to parse PR info: %w", err)
	}

	if result.Number == 0 {
		return 0, "", "", "", fmt.Errorf("PR number is zero or missing")
	}

	if result.URL == "" {
		return 0, "", "", "", fmt.Errorf("PR URL is missing")
	}

	// Parse owner/repo/host from URL like https://github.com/owner/repo/pull/123
	owner, repo, host, prNum, err := ParsePRURL(result.URL)
	if err != nil {
		return 0, "", "", "", fmt.Errorf("failed to parse PR URL: %w", err)
	}

	// Sanity check: parsed PR number should match
	if prNum != result.Number {
		return 0, "", "", "", fmt.Errorf("PR number mismatch: URL has %d, JSON has %d", prNum, result.Number)
	}

	return result.Number, owner, repo, host, nil
}

// GetCurrentPRWithRepo auto-detects PR number, repository, and host from
// current branch. This correctly handles forked repos by getting owner/repo
// from the PR URL rather than from the local git remote. In jj (Jujutsu)
// repos, sets GIT_DIR so that gh pr view can locate the git repository.
// The host comes from the PR URL, so enterprise checkouts resolve against
// the enterprise instance.
func GetCurrentPRWithRepo() (int, string, string, string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,url")
	SetGITDirForJJ(cmd)
	output, err := cmd.Output()
	if err != nil {
		return 0, "", "", "", fmt.Errorf("not on a PR branch or gh CLI not available")
	}

	return parsePRViewWithRepo(output)
}

// GetPRWithRepo fetches PR number, repository, and host for an explicit PR
// number. This correctly handles forked repos by getting owner/repo from the
// PR URL. In jj (Jujutsu) repos, sets GIT_DIR so that gh pr view can locate
// the git repository. The host comes from the PR URL.
func GetPRWithRepo(prNumber int) (int, string, string, string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--json", "number,url")
	SetGITDirForJJ(cmd)
	output, err := cmd.Output()
	if err != nil {
		return 0, "", "", "", fmt.Errorf("failed to view PR #%d: %w", prNumber, err)
	}

	return parsePRViewWithRepo(output)
}

// ParseActionsRunURL extracts host, owner, repo, and run ID from a GitHub
// Actions run URL. Expected format:
// https://github.com/owner/repo/actions/runs/NNN — any host is accepted
// (GitHub Enterprise URLs included); unknown hosts fail later at the
// auth/API layer. A run ID of 0 is rejected (found by FuzzParseActionsRunURL).
func ParseActionsRunURL(url string) (host, owner, repo string, runID int64, err error) {
	matches := actionsRunURLPattern.FindStringSubmatch(url)
	if len(matches) != 5 {
		return "", "", "", 0, fmt.Errorf("invalid Actions run URL: %s (expected https://github.com/owner/repo/actions/runs/NNN)", url)
	}
	id, err := strconv.ParseInt(matches[4], 10, 64)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("invalid run ID: %w", err)
	}
	if id <= 0 {
		return "", "", "", 0, fmt.Errorf("invalid run ID %d in URL: %s", id, url)
	}
	return strings.ToLower(matches[1]), matches[2], matches[3], id, nil
}

// ParsePRURL extracts host, owner, repo, and PR number from a GitHub PR URL.
// Any host is accepted (GitHub Enterprise URLs included); unknown hosts fail
// later at the auth/API layer. A PR number of 0 is rejected (found by
// FuzzParsePRURL).
func ParsePRURL(prURL string) (host, owner, repo string, prNumber int, err error) {
	matches := prURLPattern.FindStringSubmatch(prURL)
	if len(matches) != 5 {
		return "", "", "", 0, fmt.Errorf("invalid PR URL: %s (expected https://github.com/owner/repo/pull/NNN)", prURL)
	}
	prNum, err := strconv.Atoi(matches[4])
	if err != nil {
		return "", "", "", 0, fmt.Errorf("invalid PR number: %w", err)
	}
	if prNum <= 0 {
		return "", "", "", 0, fmt.Errorf("invalid PR number %d in URL: %s", prNum, prURL)
	}
	return strings.ToLower(matches[1]), matches[2], matches[3], prNum, nil
}

// FetchPRInfo retrieves metadata about a pull request. Only the PR-level
// fields (number, title, head SHA, created-at) come from REST; the head
// commit's push time is sourced from the GraphQL check-runs query
// (FetchCheckRunsGraphQL) which fetches pushedDate in the same round-trip
// as the StatusCheckRollup. Callers that need the push time should consume
// the time.Time returned by FetchCheckRunsGraphQL.
func FetchPRInfo(ctx context.Context, client *github.Client, owner, repo string, prNumber int) (*PRInfo, error) {
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PR #%d: %w", prNumber, err)
	}

	headSHA := pr.GetHead().GetSHA()

	return &PRInfo{
		Number:    prNumber,
		Title:     pr.GetTitle(),
		HeadSHA:   headSHA,
		CreatedAt: pr.GetCreatedAt().Format(TimestampFormat),
	}, nil
}

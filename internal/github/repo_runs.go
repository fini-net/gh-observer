package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v88/github"
)

// BranchRunData holds a standalone (non-PR) workflow run and its jobs.
// Used by repo mode to show post-merge, scheduled, and workflow_dispatch runs
// grouped by branch separately from PR check rolls.
type BranchRunData struct {
	RunID        int64
	DisplayTitle string
	HeadBranch   string
	Event        string
	WorkflowName string
	WorkflowID   int64
	Status       string
	Conclusion   string
	CreatedAt    time.Time
	RunStartedAt time.Time
	Jobs         []CheckRunInfo
}

// FetchRepoWorkflowRuns lists standalone (non-PR) workflow runs on a repo that
// are either currently active or completed within fadeWindow. It issues two
// ListRepositoryWorkflowRuns calls (in_progress, then recently-created) with
// ExcludePullRequests set so PR-triggered runs are not double-counted against
// the PR GraphQL query.
//
// Returns the deduplicated run list and the minimum rate-limit remaining observed.
func FetchRepoWorkflowRuns(ctx context.Context, client *github.Client, owner, repo string, fadeWindow time.Duration) ([]BranchRunData, int, error) {
	rateLimitRemaining := 5000

	inProgress := &github.ListWorkflowRunsOptions{
		ExcludePullRequests: true,
		Status:              "in_progress",
		ListOptions:         github.ListOptions{PerPage: 100},
	}
	activeRuns, rl1, err := fetchRepoRunPage(ctx, client, owner, repo, inProgress)
	if err != nil {
		return nil, rateLimitRemaining, err
	}
	if rl1 < rateLimitRemaining {
		rateLimitRemaining = rl1
	}

	// Recently completed: filter by creation date to bound the result set.
	recent := &github.ListWorkflowRunsOptions{
		ExcludePullRequests: true,
		Created:              ">=" + time.Now().Add(-fadeWindow).Format(time.DateOnly),
		ListOptions:         github.ListOptions{PerPage: 100},
	}
	completedRuns, rl2, err := fetchRepoRunPage(ctx, client, owner, repo, recent)
	if err != nil {
		debug.Log("failed to fetch completed repo runs", "owner", owner, "repo", repo, "err", err)
		return activeRuns, rateLimitRemaining, nil
	}
	if rl2 < rateLimitRemaining {
		rateLimitRemaining = rl2
	}

	seen := make(map[int64]bool)
	var result []BranchRunData
	for _, r := range activeRuns {
		if !seen[r.RunID] {
			seen[r.RunID] = true
			result = append(result, r)
		}
	}
	for _, r := range completedRuns {
		if !seen[r.RunID] {
			seen[r.RunID] = true
			result = append(result, r)
		}
	}

	debug.Log("fetched repo workflow runs", "owner", owner, "repo", repo,
		"active", len(activeRuns), "recent", len(completedRuns),
		"total", len(result), "rate_limit_remaining", rateLimitRemaining)

	return result, rateLimitRemaining, nil
}

func fetchRepoRunPage(ctx context.Context, client *github.Client, owner, repo string, opts *github.ListWorkflowRunsOptions) ([]BranchRunData, int, error) {
	var allRuns []BranchRunData
	rateLimitRemaining := 5000

	for {
		runs, resp, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
		if err != nil {
			return nil, rateLimitRemaining, fmt.Errorf("failed to list repo workflow runs: %w", err)
		}
		if resp != nil {
			rateLimitRemaining = resp.Rate.Remaining
		}
		for _, run := range runs.WorkflowRuns {
			allRuns = append(allRuns, convertBranchRun(run))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return allRuns, rateLimitRemaining, nil
}

func convertBranchRun(run *github.WorkflowRun) BranchRunData {
	data := BranchRunData{
		RunID:      run.GetID(),
		HeadBranch: run.GetHeadBranch(),
		Event:      run.GetEvent(),
		WorkflowID: run.GetWorkflowID(),
		Status:     strings.ToLower(run.GetStatus()),
		Conclusion: strings.ToLower(run.GetConclusion()),
	}
	if run.DisplayTitle != nil && *run.DisplayTitle != "" {
		data.DisplayTitle = *run.DisplayTitle
	} else if run.Name != nil {
		data.DisplayTitle = *run.Name
	}
	if run.CreatedAt != nil {
		data.CreatedAt = run.CreatedAt.Time
	}
	if run.RunStartedAt != nil {
		data.RunStartedAt = run.RunStartedAt.Time
	}
	return data
}

// EnrichRepoRunsWithJobs fetches jobs for each run via FetchRunJobs and adapts
// them to CheckRunInfo so the TUI can reuse display helpers. WorkflowName is
// copied from the first job (GitHub populates it on WorkflowJob).
//
// Failures on individual runs are non-fatal: the run is kept with an empty
// Jobs slice so the TUI can still render its header.
func EnrichRepoRunsWithJobs(ctx context.Context, client *github.Client, owner, repo string, runs []BranchRunData) ([]BranchRunData, int, error) {
	rateLimitRemaining := 5000
	for i := range runs {
		jobs, rl, err := FetchRunJobs(ctx, client, owner, repo, runs[i].RunID)
		if err != nil {
			debug.Log("failed to fetch jobs for repo run", "run_id", runs[i].RunID, "err", err)
			continue
		}
		if rl < rateLimitRemaining {
			rateLimitRemaining = rl
		}

		checkRuns := WorkflowJobInfoToCheckRuns(jobs)
		runs[i].Jobs = checkRuns

		if len(checkRuns) > 0 && checkRuns[0].WorkflowName != "" {
			runs[i].WorkflowName = checkRuns[0].WorkflowName
		}
	}
	return runs, rateLimitRemaining, nil
}
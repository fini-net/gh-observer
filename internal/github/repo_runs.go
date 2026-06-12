package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v86/github"
)

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

var activeStatuses = map[string]bool{
	"in_progress": true,
	"queued":      true,
	"waiting":     true,
	"pending":    true,
}

func isActiveRun(status string) bool {
	return activeStatuses[strings.ToLower(status)]
}

func isRecentlyCompleted(run *github.WorkflowRun, fadeWindow time.Duration) bool {
	if run.UpdatedAt == nil {
		return false
	}
	return time.Since(run.UpdatedAt.Time) < fadeWindow
}

func includeRun(run *github.WorkflowRun, fadeWindow time.Duration) bool {
	status := strings.ToLower(run.GetStatus())
	if isActiveRun(status) {
		return true
	}
	return isRecentlyCompleted(run, fadeWindow)
}

func FetchBranchRuns(ctx context.Context, client *github.Client, owner, repo, branch string, fadeWindow time.Duration) ([]BranchRunData, int, error) {
	opts := &github.ListWorkflowRunsOptions{
		Branch:              branch,
		ExcludePullRequests: true,
		Status:              "in_progress",
		ListOptions:         github.ListOptions{PerPage: 100},
	}

	inProgressRuns, rateLimitRemaining, err := fetchBranchRunPage(ctx, client, owner, repo, opts)
	if err != nil {
		return nil, rateLimitRemaining, err
	}

	opts.Status = ""
	opts.Created = ">=" + time.Now().Add(-fadeWindow).Format(time.DateOnly)

	completedRuns, rateLimit2, err := fetchBranchRunPage(ctx, client, owner, repo, opts)
	if err != nil {
		debug.Log("failed to fetch completed branch runs", "err", err)
		return inProgressRuns, rateLimitRemaining, nil
	}

	if rateLimit2 < rateLimitRemaining {
		rateLimitRemaining = rateLimit2
	}

	seen := make(map[int64]bool)
	var result []BranchRunData
	for _, r := range inProgressRuns {
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

	debug.Log("fetched branch runs", "owner", owner, "repo", repo, "branch", branch,
		"in_progress", len(inProgressRuns), "completed", len(completedRuns),
		"total", len(result), "rate_limit_remaining", rateLimitRemaining)

	return result, rateLimitRemaining, nil
}

func fetchBranchRunPage(ctx context.Context, client *github.Client, owner, repo string, opts *github.ListWorkflowRunsOptions) ([]BranchRunData, int, error) {
	var allRuns []BranchRunData
	rateLimitRemaining := 5000

	for {
		runs, resp, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
		if err != nil {
			return nil, rateLimitRemaining, fmt.Errorf("failed to fetch branch workflow runs: %w", err)
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
		RunID:        run.GetID(),
		DisplayTitle: run.GetDisplayTitle(),
		HeadBranch:   run.GetHeadBranch(),
		Event:        run.GetEvent(),
		WorkflowID:   run.GetWorkflowID(),
		Status:       strings.ToLower(run.GetStatus()),
		Conclusion:   strings.ToLower(run.GetConclusion()),
	}
	if data.DisplayTitle == "" && run.Name != nil {
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

func EnrichBranchRunsWithJobs(ctx context.Context, client *github.Client, owner, repo string, runs []BranchRunData) ([]BranchRunData, int, error) {
	rateLimitRemaining := 5000
	for i := range runs {
		jobs, rl, err := FetchRunJobs(ctx, client, owner, repo, runs[i].RunID)
		if err != nil {
			debug.Log("failed to fetch jobs for branch run", "run_id", runs[i].RunID, "err", err)
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
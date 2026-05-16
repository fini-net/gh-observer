package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v86/github"
)

// RepositoryRunInfo contains summary data for a workflow run in repo-watch mode.
type RepositoryRunInfo struct {
	ID             int64
	DisplayTitle   string
	WorkflowName   string
	Status         string
	Conclusion     string
	HeadSHA        string
	HeadBranch     string
	CreatedAt      *github.Timestamp
	RunStartedAt   *github.Timestamp
	UpdatedAt      *github.Timestamp
	HTMLURL        string
}

// FetchRepositoryRuns fetches recent workflow runs for a repository.
// Returns runs (most recent first), rate limit remaining, and any error.
func FetchRepositoryRuns(ctx context.Context, client *github.Client, owner, repo string) ([]RepositoryRunInfo, int, error) {
	opts := &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 30},
	}

	var allRuns []RepositoryRunInfo
	rateLimitRemaining := 5000

	for {
		runs, resp, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
		if err != nil {
			return nil, rateLimitRemaining, fmt.Errorf("failed to list repository workflow runs: %w", err)
		}

		if resp != nil {
			rateLimitRemaining = resp.Rate.Remaining
		}

		for _, run := range runs.WorkflowRuns {
			info := RepositoryRunInfo{
				ID:     run.GetID(),
				Status: strings.ToLower(run.GetStatus()),
			}

			if run.Name != nil {
				info.DisplayTitle = *run.Name
			}
			if run.DisplayTitle != nil && *run.DisplayTitle != "" {
				info.DisplayTitle = *run.DisplayTitle
			}
			if run.Conclusion != nil {
				info.Conclusion = strings.ToLower(*run.Conclusion)
			}
			if run.HeadSHA != nil {
				info.HeadSHA = *run.HeadSHA
			}
			if run.HeadBranch != nil {
				info.HeadBranch = *run.HeadBranch
			}
			info.CreatedAt = run.CreatedAt
			info.RunStartedAt = run.RunStartedAt
			info.UpdatedAt = run.UpdatedAt
			if run.HTMLURL != nil {
				info.HTMLURL = *run.HTMLURL
			}

			allRuns = append(allRuns, info)
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	debug.Log("fetch repository runs", "owner", owner, "repo", repo, "count", len(allRuns), "rate_limit_remaining", rateLimitRemaining)

	return allRuns, rateLimitRemaining, nil
}

// FailureRunConclusion returns true if the conclusion indicates a failed run.
func FailureRunConclusion(conclusion string) bool {
	return conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required"
}

// AllRunsComplete returns true if all runs have finished.
func AllRunsComplete(runs []RepositoryRunInfo) bool {
	if len(runs) == 0 {
		return false
	}
	for _, run := range runs {
		if run.Status != "completed" {
			return false
		}
	}
	return true
}

// DetermineRepoWatchExitCode returns 1 if any run failed, 0 otherwise.
func DetermineRepoWatchExitCode(runs []RepositoryRunInfo) int {
	for _, run := range runs {
		if FailureRunConclusion(run.Conclusion) {
			return 1
		}
	}
	return 0
}

// WorkflowJobInfo contains status data for a single job within a workflow run.
type WorkflowJobInfo struct {
	Name         string
	WorkflowName string
	Status       string
	Conclusion   string
	StartedAt    *github.Timestamp
	CompletedAt  *github.Timestamp
	HTMLURL      string
	RunID        int64
	WorkflowID   int64
}

// RunInfo contains metadata about a workflow run (for the header display).
type RunInfo struct {
	ID             int64
	DisplayTitle   string
	HeadSHA        string
	HeadCommitMsg  string
	HeadCommitTime *github.Timestamp
	CreatedAt      *github.Timestamp
	RunStartedAt   *github.Timestamp
	Status         string
	Conclusion     string
	WorkflowID     int64
}

// firstLine returns the first line of a multiline string, trimmed.
func firstLine(s string) string {
	if s == "" {
		return ""
	}
	line := strings.SplitN(s, "\n", 2)[0]
	return strings.TrimSpace(line)
}

// FetchRunInfo retrieves metadata for a workflow run by its ID.
func FetchRunInfo(ctx context.Context, client *github.Client, owner, repo string, runID int64) (*RunInfo, error) {
	run, _, err := client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow run %d: %w", runID, err)
	}

	info := &RunInfo{
		ID:       run.GetID(),
		Status:   run.GetStatus(),
		WorkflowID: run.GetWorkflowID(),
	}

	if run.Name != nil {
		info.DisplayTitle = *run.Name
	}
	if run.DisplayTitle != nil && *run.DisplayTitle != "" {
		info.DisplayTitle = *run.DisplayTitle
	}
	if run.HeadSHA != nil {
		info.HeadSHA = *run.HeadSHA
	}
	if run.HeadCommit != nil {
		if run.HeadCommit.Message != nil {
			info.HeadCommitMsg = firstLine(*run.HeadCommit.Message)
		}
		info.HeadCommitTime = run.HeadCommit.Timestamp
	}
	if run.CreatedAt != nil {
		info.CreatedAt = run.CreatedAt
	}
	if run.RunStartedAt != nil {
		info.RunStartedAt = run.RunStartedAt
	}
	if run.Conclusion != nil {
		info.Conclusion = *run.Conclusion
	}

	debug.Log("fetch run info", "run_id", runID, "name", info.DisplayTitle, "status", info.Status)

	return info, nil
}

// FetchRunJobs retrieves the jobs for a workflow run by its ID.
// Returns the jobs, rate limit remaining, and any error.
func FetchRunJobs(ctx context.Context, client *github.Client, owner, repo string, runID int64) ([]WorkflowJobInfo, int, error) {
	opts := &github.ListWorkflowJobsOptions{
		Filter:      "latest",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allJobs []WorkflowJobInfo
	rateLimitRemaining := 5000

	for {
		jobs, resp, err := client.Actions.ListWorkflowJobs(ctx, owner, repo, runID, opts)
		if err != nil {
			return nil, rateLimitRemaining, fmt.Errorf("failed to fetch jobs for run %d: %w", runID, err)
		}

		if resp != nil {
			rateLimitRemaining = resp.Rate.Remaining
		}

		for _, job := range jobs.Jobs {
			allJobs = append(allJobs, convertWorkflowJob(job))
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	debug.Log("fetch run jobs", "run_id", runID, "count", len(allJobs), "rate_limit_remaining", rateLimitRemaining)

	return allJobs, rateLimitRemaining, nil
}

// convertWorkflowJob converts a go-github WorkflowJob to our WorkflowJobInfo.
func convertWorkflowJob(job *github.WorkflowJob) WorkflowJobInfo {
	info := WorkflowJobInfo{
		Status:     strings.ToLower(job.GetStatus()),
		Conclusion: strings.ToLower(job.GetConclusion()),
	}

	if job.Name != nil {
		info.Name = *job.Name
	}
	if job.WorkflowName != nil {
		info.WorkflowName = *job.WorkflowName
	}
	if job.HTMLURL != nil {
		info.HTMLURL = *job.HTMLURL
	}
	if job.RunID != nil {
		info.RunID = *job.RunID
	}
	info.StartedAt = job.StartedAt
	info.CompletedAt = job.CompletedAt

	return info
}

// WorkflowJobInfoToCheckRuns converts a slice of WorkflowJobInfo to CheckRunInfo
// for use with existing discovery and history-fetching functions.
func WorkflowJobInfoToCheckRuns(jobs []WorkflowJobInfo) []CheckRunInfo {
	var runs []CheckRunInfo
	for _, job := range jobs {
		cr := CheckRunInfo{
			Name:          job.Name,
			WorkflowName:  job.WorkflowName,
			Status:        job.Status,
			Conclusion:    job.Conclusion,
			DetailsURL:   job.HTMLURL,
			WorkflowRunID: job.RunID,
			WorkflowID:    job.WorkflowID,
		}
		if job.StartedAt != nil {
			t := job.StartedAt.Time
			cr.StartedAt = &t
		}
		if job.CompletedAt != nil {
			t := job.CompletedAt.Time
			cr.CompletedAt = &t
		}
		runs = append(runs, cr)
	}
	return runs
}

// FailureJobConclusion returns true if the conclusion indicates a failed job.
func FailureJobConclusion(conclusion string) bool {
	return conclusion == "failure" || conclusion == "timed_out" || conclusion == "action_required"
}

// AllJobsComplete returns true if all jobs have finished.
func AllJobsComplete(jobs []WorkflowJobInfo) bool {
	if len(jobs) == 0 {
		return false
	}
	for _, job := range jobs {
		if job.Status != "completed" {
			return false
		}
	}
	return true
}

// DetermineRunExitCode returns 1 if any job failed, 0 otherwise.
func DetermineRunExitCode(jobs []WorkflowJobInfo) int {
	for _, job := range jobs {
		if FailureJobConclusion(job.Conclusion) {
			return 1
		}
	}
	return 0
}
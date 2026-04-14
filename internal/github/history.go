package github

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/google/go-github/v84/github"
)

var runIDRegexp = regexp.MustCompile(`/actions/runs/(\d+)/job/`)

// ParseRunIDFromURL extracts the workflow run ID from a GitHub Actions details URL.
func ParseRunIDFromURL(detailsURL string) (int64, error) {
	matches := runIDRegexp.FindStringSubmatch(detailsURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("no run ID found in URL: %s", detailsURL)
	}
	return strconv.ParseInt(matches[1], 10, 64)
}

// FetchJobAverages fetches historical average durations for each job.
// Returns a map keyed by bare job name to average duration.
// Non-fatal: skips failed calls and returns whatever data was collected.
//
// The knownRunIDToWorkflowID and knownFetchedWorkflowIDs parameters enable incremental
// fetching: run IDs already mapped to workflow IDs are cached, and workflow IDs already
// fetched are skipped. New mappings and newly-fetched workflow IDs are returned for caching.
func FetchJobAverages(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	checkRuns []CheckRunInfo,
	knownRunIDToWorkflowID map[int64]int64,
	knownFetchedWorkflowIDs map[int64]bool,
) (
	averages map[string]time.Duration,
	newRunIDToWorkflowID map[int64]int64,
	newFetchedWorkflowIDs []int64,
	err error,
) {
	// Step 1: collect unique run IDs from check run URLs
	runIDSet := map[int64]bool{}
	for _, cr := range checkRuns {
		if cr.DetailsURL == "" {
			continue
		}
		runID, err := ParseRunIDFromURL(cr.DetailsURL)
		if err != nil {
			continue
		}
		runIDSet[runID] = true
	}

	if len(runIDSet) == 0 {
		return nil, nil, nil, nil
	}

	// Step 2: per unique run_id, get workflow_id (using cache where available)
	newRunIDToWorkflowID = make(map[int64]int64)
	workflowIDSet := map[int64]bool{}

	for runID := range runIDSet {
		// Check cache first
		if wfID, ok := knownRunIDToWorkflowID[runID]; ok {
			workflowIDSet[wfID] = true
			continue
		}

		// Fetch workflow ID for new run ID
		run, _, err := client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
		if err != nil {
			continue
		}
		if run.WorkflowID != nil {
			workflowIDSet[*run.WorkflowID] = true
			newRunIDToWorkflowID[runID] = *run.WorkflowID
		}
	}

	if len(workflowIDSet) == 0 {
		return nil, newRunIDToWorkflowID, nil, nil
	}

	// Step 3: filter out already-fetched workflow IDs
	workflowIDsToFetch := make([]int64, 0, len(workflowIDSet))
	for wfID := range workflowIDSet {
		if !knownFetchedWorkflowIDs[wfID] {
			workflowIDsToFetch = append(workflowIDsToFetch, wfID)
		}
	}

	if len(workflowIDsToFetch) == 0 {
		// All workflows already fetched, nothing new to process
		return nil, newRunIDToWorkflowID, nil, nil
	}

	// Step 4: per workflow_id to fetch, get recent completed run IDs
	var historicalRunIDs []int64
	for _, wfID := range workflowIDsToFetch {
		runs, _, err := client.Actions.ListWorkflowRunsByID(ctx, owner, repo, wfID, &github.ListWorkflowRunsOptions{
			Status:      "completed",
			ListOptions: github.ListOptions{PerPage: 10},
		})
		if err != nil {
			continue
		}
		for _, run := range runs.WorkflowRuns {
			if run.ID != nil {
				historicalRunIDs = append(historicalRunIDs, *run.ID)
			}
		}
	}

	if len(historicalRunIDs) == 0 {
		return nil, newRunIDToWorkflowID, workflowIDsToFetch, nil
	}

	averages = averageJobDurations(ctx, client, owner, repo, historicalRunIDs)

	return averages, newRunIDToWorkflowID, workflowIDsToFetch, nil
}

// averageJobDurations fetches jobs for the given run IDs and returns
// averaged durations per job name.
func averageJobDurations(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	runIDs []int64,
) map[string]time.Duration {
	jobDurations := map[string][]time.Duration{}
	for _, runID := range runIDs {
		jobs, _, err := client.Actions.ListWorkflowJobs(ctx, owner, repo, runID, &github.ListWorkflowJobsOptions{
			Filter:      "latest",
			ListOptions: github.ListOptions{PerPage: 100},
		})
		if err != nil {
			continue
		}
		for _, job := range jobs.Jobs {
			if job.Name == nil || job.StartedAt == nil || job.CompletedAt == nil {
				continue
			}
			dur := job.CompletedAt.Sub(job.StartedAt.Time)
			if dur > 0 {
				jobDurations[*job.Name] = append(jobDurations[*job.Name], dur)
			}
		}
	}

	if len(jobDurations) == 0 {
		return nil
	}

	averages := make(map[string]time.Duration, len(jobDurations))
	for name, durations := range jobDurations {
		var total time.Duration
		for _, d := range durations {
			total += d
		}
		averages[name] = total / time.Duration(len(durations))
	}

	return averages
}

// DiscoverWorkflows resolves run IDs to workflow IDs.
// Returns new run ID → workflow ID mappings and the list of workflow IDs that need fetching.
func DiscoverWorkflows(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	checkRuns []CheckRunInfo,
	knownRunIDToWorkflowID map[int64]int64,
	knownFetchedWorkflowIDs map[int64]bool,
) (
	newRunIDToWorkflowID map[int64]int64,
	workflowIDsToFetch []int64,
	err error,
) {
	runIDSet := map[int64]bool{}
	for _, cr := range checkRuns {
		if cr.DetailsURL == "" {
			continue
		}
		runID, parseErr := ParseRunIDFromURL(cr.DetailsURL)
		if parseErr != nil {
			continue
		}
		runIDSet[runID] = true
	}

	if len(runIDSet) == 0 {
		return nil, nil, nil
	}

	newRunIDToWorkflowID = make(map[int64]int64)
	workflowIDSet := map[int64]bool{}

	for runID := range runIDSet {
		if wfID, ok := knownRunIDToWorkflowID[runID]; ok {
			workflowIDSet[wfID] = true
			continue
		}

		run, _, apiErr := client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
		if apiErr != nil {
			continue
		}
		if run.WorkflowID != nil {
			workflowIDSet[*run.WorkflowID] = true
			newRunIDToWorkflowID[runID] = *run.WorkflowID
		}
	}

	for wfID := range workflowIDSet {
		if !knownFetchedWorkflowIDs[wfID] {
			workflowIDsToFetch = append(workflowIDsToFetch, wfID)
		}
	}

	return newRunIDToWorkflowID, workflowIDsToFetch, nil
}

// FetchWorkflowHistory fetches historical job durations for a single workflow.
// Returns averaged durations per job name for the given workflow.
func FetchWorkflowHistory(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	workflowID int64,
) (map[string]time.Duration, error) {
	runs, _, err := client.Actions.ListWorkflowRunsByID(ctx, owner, repo, workflowID, &github.ListWorkflowRunsOptions{
		Status:      "completed",
		ListOptions: github.ListOptions{PerPage: 10},
	})
	if err != nil {
		return nil, err
	}

	var historicalRunIDs []int64
	for _, run := range runs.WorkflowRuns {
		if run.ID != nil {
			historicalRunIDs = append(historicalRunIDs, *run.ID)
		}
	}

	if len(historicalRunIDs) == 0 {
		return nil, nil
	}

	averages := averageJobDurations(ctx, client, owner, repo, historicalRunIDs)

	return averages, nil
}

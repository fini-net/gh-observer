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
var jobIDRegexp = regexp.MustCompile(`/job/(\d+)$`)

// ParseRunIDFromURL extracts the workflow run ID from a GitHub Actions details URL.
func ParseRunIDFromURL(detailsURL string) (int64, error) {
	matches := runIDRegexp.FindStringSubmatch(detailsURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("no run ID found in URL: %s", detailsURL)
	}
	return strconv.ParseInt(matches[1], 10, 64)
}

// ParseJobIDFromURL extracts the job ID from a GitHub Actions details URL.
func ParseJobIDFromURL(detailsURL string) (int64, error) {
	matches := jobIDRegexp.FindStringSubmatch(detailsURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("no job ID found in URL: %s", detailsURL)
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

	// Step 5: per historical run_id, collect job durations by name
	jobDurations := map[string][]time.Duration{}
	for _, runID := range historicalRunIDs {
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
			dur := job.CompletedAt.Time.Sub(job.StartedAt.Time)
			if dur > 0 {
				jobDurations[*job.Name] = append(jobDurations[*job.Name], dur)
			}
		}
	}

	// Step 6: average durations per job name
	averages = make(map[string]time.Duration, len(jobDurations))
	for name, durations := range jobDurations {
		var total time.Duration
		for _, d := range durations {
			total += d
		}
		averages[name] = total / time.Duration(len(durations))
	}

	return averages, newRunIDToWorkflowID, workflowIDsToFetch, nil
}

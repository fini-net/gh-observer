package github

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v88/github"
)

var runIDRegexp = regexp.MustCompile(`/actions/runs/(\d+)/job/`)

// historyDecayFactor weights recent runs more heavily than older ones in
// weightedAverage. Each run i (0 = newest) contributes historyDecayFactor^i
// of the average. 0.7 gives the newest run ~42% of a 10-run average and the
// oldest ~1%, so a slow run from two weeks ago no longer drags the ETA above
// where the next run actually lands.
const historyDecayFactor = 0.7

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
	// Step 1: collect unique workflow run IDs and directly-known workflow IDs
	newRunIDToWorkflowID = make(map[int64]int64)
	runIDSet := map[int64]bool{}
	directWorkflowIDSet := map[int64]bool{}
	for _, cr := range checkRuns {
		if cr.WorkflowID > 0 {
			directWorkflowIDSet[cr.WorkflowID] = true
			if cr.WorkflowRunID > 0 {
				runIDSet[cr.WorkflowRunID] = true
				newRunIDToWorkflowID[cr.WorkflowRunID] = cr.WorkflowID
			}
			continue
		}
		if cr.WorkflowRunID > 0 {
			runIDSet[cr.WorkflowRunID] = true
			continue
		}
		if cr.DetailsURL == "" {
			continue
		}
		runID, err := ParseRunIDFromURL(cr.DetailsURL)
		if err != nil {
			continue
		}
		runIDSet[runID] = true
	}

	if len(runIDSet) == 0 && len(directWorkflowIDSet) == 0 {
		return nil, nil, nil, nil
	}

	// Step 2: per unique run_id, get workflow_id (using cache where available)
	workflowIDSet := map[int64]bool{}
	for wfID := range directWorkflowIDSet {
		workflowIDSet[wfID] = true
	}

	for runID := range runIDSet {
		if wfID, alreadyKnown := newRunIDToWorkflowID[runID]; alreadyKnown {
			workflowIDSet[wfID] = true
			continue
		}
		// Check cache first
		if wfID, ok := knownRunIDToWorkflowID[runID]; ok {
			debug.Log("workflow cache hit (fetch)", "run_id", runID, "workflow_id", wfID)
			workflowIDSet[wfID] = true
			continue
		}

		// Fetch workflow ID for new run ID
		run, _, err := client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
		if err != nil {
			debug.Log("workflow run fetch failed", "run_id", runID, "err", err)
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
			debug.Log("workflow runs list failed", "workflow_id", wfID, "err", err)
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

	// durations slices are newest-first: runIDs come from ListWorkflowRunsByID
	// (which returns runs newest-first when Status="completed"), and each run's
	// jobs are appended together via ListWorkflowJobs. weightedAverage relies on
	// this ordering to give the most recent run the largest weight, so any
	// future refactor that re-shuffles runIDs or durations must preserve it.
	averages := make(map[string]time.Duration, len(jobDurations))
	for name, durations := range jobDurations {
		averages[name] = weightedAverage(durations)
	}

	return averages
}

// weightedAverage computes an exponentially decayed weighted average of
// durations, treating index 0 as the newest (most weight) and later indices as
// progressively older. With a single duration it returns that duration
// unchanged; with an empty slice it returns 0. See historyDecayFactor for the
// decay constant.
//
// Ordering invariant: durations MUST be newest-first. averageJobDurations
// preserves this ordering (see its comment), and the weighting depends on it.
func weightedAverage(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var weightedSum, weightSum, weight float64 = 0, 0, 1
	for _, d := range durations {
		weightedSum += weight * float64(d)
		weightSum += weight
		weight *= historyDecayFactor
	}
	return time.Duration(weightedSum / weightSum)
}

// DiscoverWorkflows resolves run IDs to workflow IDs.
// Uses WorkflowRunID/WorkflowID from GraphQL when available, falling back to
// ParseRunIDFromURL for backward compatibility.
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
	newRunIDToWorkflowID = make(map[int64]int64)
	workflowIDSet := map[int64]bool{}

	for _, cr := range checkRuns {
		if cr.WorkflowID > 0 {
			workflowIDSet[cr.WorkflowID] = true
			if cr.WorkflowRunID > 0 {
				newRunIDToWorkflowID[cr.WorkflowRunID] = cr.WorkflowID
			}
			debug.Log("discover workflow ID from GraphQL", "workflow_id", cr.WorkflowID, "workflow_name", cr.WorkflowName)
			continue
		}

		if cr.WorkflowRunID > 0 {
			if wfID, ok := knownRunIDToWorkflowID[cr.WorkflowRunID]; ok {
				debug.Log("discover cache hit (GraphQL run ID)", "run_id", cr.WorkflowRunID, "workflow_id", wfID)
				workflowIDSet[wfID] = true
				continue
			}
			run, _, apiErr := client.Actions.GetWorkflowRunByID(ctx, owner, repo, cr.WorkflowRunID)
			if apiErr != nil {
				debug.Log("discover workflow run fetch failed", "run_id", cr.WorkflowRunID, "err", apiErr)
				continue
			}
			if run.WorkflowID != nil {
				workflowIDSet[*run.WorkflowID] = true
				newRunIDToWorkflowID[cr.WorkflowRunID] = *run.WorkflowID
			}
			continue
		}

		if cr.DetailsURL == "" {
			continue
		}
		runID, parseErr := ParseRunIDFromURL(cr.DetailsURL)
		if parseErr != nil {
			debug.Log("discover run ID parse skipped", "url", cr.DetailsURL, "err", parseErr)
			continue
		}
		if wfID, ok := knownRunIDToWorkflowID[runID]; ok {
			debug.Log("discover cache hit", "run_id", runID, "workflow_id", wfID)
			workflowIDSet[wfID] = true
			continue
		}

		run, _, apiErr := client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
		if apiErr != nil {
			debug.Log("discover workflow run fetch failed", "run_id", runID, "err", apiErr)
			continue
		}
		if run.WorkflowID != nil {
			workflowIDSet[*run.WorkflowID] = true
			newRunIDToWorkflowID[runID] = *run.WorkflowID
		}
	}

	if len(workflowIDSet) == 0 {
		return nil, nil, nil
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
		debug.Log("fetch workflow history failed", "workflow_id", workflowID, "err", err)
		return nil, err
	}

	debug.Log("fetch workflow history", "workflow_id", workflowID, "runs", len(runs.WorkflowRuns))

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

// DiscoverAdvSecWorkflows matches GitHub Advanced Security checks to their
// corresponding github-actions workflows by name. Returns a map of AdvSec
// check Name → matched WorkflowID and the list of additional workflow IDs
// that need history fetching.
//
// GitHub Advanced Security checks (CodeQL, Checkov, etc.) have workflowRun=null
// in GraphQL, but their Name typically matches the WorkflowName of a
// github-actions check run in the same PR. For example:
//   - AdvSec check Name="CodeQL" matches github-actions WorkflowName="CodeQL"
//   - AdvSec check Name="Checkov" matches github-actions WorkflowName="Checkov"
func DiscoverAdvSecWorkflows(
	checkRuns []CheckRunInfo,
	knownFetchedWorkflowIDs map[int64]bool,
) (
	advSecMatchWorkflow map[string]int64,
	workflowIDsToFetch []int64,
) {
	advSecMatchWorkflow = make(map[string]int64)
	workflowNameToID := map[string]int64{}

	for _, cr := range checkRuns {
		if cr.WorkflowName != "" && cr.WorkflowID > 0 {
			workflowNameToID[cr.WorkflowName] = cr.WorkflowID
		}
	}

	for _, cr := range checkRuns {
		if cr.WorkflowID > 0 || cr.WorkflowRunID > 0 {
			continue
		}
		if cr.AppName == "" || cr.DetailsURL == "" {
			continue
		}

		if matchedWFID, ok := workflowNameToID[cr.Name]; ok {
			advSecMatchWorkflow[cr.Name] = matchedWFID
			if !knownFetchedWorkflowIDs[matchedWFID] {
				workflowIDsToFetch = append(workflowIDsToFetch, matchedWFID)
			}
			debug.Log("advsec name match", "check_name", cr.Name, "workflow_id", matchedWFID)
		}
	}

	return advSecMatchWorkflow, workflowIDsToFetch
}

// githubHostedURLRegexp matches DetailsURLs that point at a GitHub-hosted
// Actions run (either a full run page or a specific job). AdvSec checks use
// /runs/<id> URLs, and some Actions checks use /actions/runs/<id> without the
// trailing /job/<id>. Both are GitHub-hosted, so they are not "external app"
// checks even when ParseRunIDFromURL (which requires /job/) cannot recover a
// run ID from them. Treating them as external would let a user-supplied
// presumed average shadow the real history that AdvSec aliasing later writes.
var githubHostedURLRegexp = regexp.MustCompile(`^https?://github\.com/[^/]+/[^/]+/(actions/runs/|runs/)`)

// IsExternalAppCheck reports whether a check run is from an external (non-GitHub
// Actions) app — i.e., it has no WorkflowRunID and no WorkflowID, but has both
// an AppName and a DetailsURL that does not point at a GitHub-hosted Actions or
// AdvSec run. The DCO app provided by Probot is the canonical example; its
// DetailsURL points off-site (https://probot.github.io/apps/dco/) so neither
// ParseRunIDFromURL nor githubHostedURLRegexp can recover a run ID and history
// can never be fetched for it. Such checks are candidates for a presumed
// average (see ApplyPresumedAverages).
//
// GitHub-hosted URLs (actions/runs/<id>, actions/runs/<id>/job/<id>, and AdvSec
// runs/<id>) are treated as non-external so that AdvSec aliasing in the TUI
// (which writes real history into jobAverages keyed by the check name) is not
// blocked by a presumed average having already taken the slot.
func IsExternalAppCheck(cr CheckRunInfo) bool {
	if cr.WorkflowRunID > 0 || cr.WorkflowID > 0 {
		return false
	}
	if cr.AppName == "" || cr.DetailsURL == "" {
		return false
	}
	if _, err := ParseRunIDFromURL(cr.DetailsURL); err == nil {
		return false
	}
	if githubHostedURLRegexp.MatchString(cr.DetailsURL) {
		return false
	}
	return true
}

// ApplyPresumedAverages injects presumed historical durations for checks that
// can never have real history (external GitHub App checks like DCO that have no
// Actions workflow run). For each check Name in presumedAverages that (a) is
// not already present in jobAverages and (b) appears in checkRuns as an
// external app check, the presumed duration is written into jobAverages. The
// jobAverages map is mutated in place; if it is nil it is left untouched.
//
// Matching is case-insensitive on the check Name to absorb viper's automatic
// lowercasing of map keys (the default "DCO" is stored as "dco"). The
// canonical check Name (preserving the original case from GitHub) is used as
// the jobAverages key so the rest of the TUI lookup continues to work.
//
// This lets the TUI show a sensible ETA (e.g. "1s" for DCO) instead of a blank
// HistAvg cell, without making any extra API calls.
func ApplyPresumedAverages(
	jobAverages map[string]time.Duration,
	checkRuns []CheckRunInfo,
	presumedAverages map[string]time.Duration,
) {
	if len(presumedAverages) == 0 || jobAverages == nil {
		return
	}
	// Build a case-insensitive lookup so viper's lowercased map keys still
	// match the canonical check Name from the GitHub API.
	lower := make(map[string]time.Duration, len(presumedAverages))
	for name, dur := range presumedAverages {
		lower[strings.ToLower(name)] = dur
	}
	for _, cr := range checkRuns {
		if !IsExternalAppCheck(cr) {
			continue
		}
		if _, exists := jobAverages[cr.Name]; exists {
			continue
		}
		if dur, ok := lower[strings.ToLower(cr.Name)]; ok {
			jobAverages[cr.Name] = dur
			debug.Log("presumed average applied", "check_name", cr.Name, "duration", dur)
		}
	}
}

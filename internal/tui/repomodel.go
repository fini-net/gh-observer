package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// PRViewData is the per-PR view state held by RepoModel after fade-out
// filtering. HeadSHA carries the PR head commit OID so the runs handler can
// match standalone runs to a PR and dedupe/attach their jobs.
type PRViewData struct {
	Title          string
	CheckRuns      []ghclient.CheckRunInfo
	HeadCommitTime time.Time
	HeadSHA        string
}

// RepoModel holds the application state for persistent repo watching.
// It tracks both PR-associated checks (via the batched GraphQL query) and
// standalone non-PR workflow runs (via REST), applying fade-out to completed
// checks and runs so the view stays focused on what's active.
type RepoModel struct {
	ctx   context.Context
	token string
	owner string
	repo  string

	// PR data after fade-out filtering: PR number -> view data.
	prs map[int]PRViewData

	// Standalone (non-PR) workflow runs after fade-out filtering.
	standaloneRuns []ghclient.BranchRunData

	// Rate limiting
	rateLimitRemaining int

	// UI state
	spinner         spinner.Model
	startTime       time.Time
	lastUpdate      time.Time
	refreshInterval time.Duration
	styles          Styles

	// Fade-out windows for completed checks/runs.
	fadeSuccess time.Duration
	fadeFailure time.Duration

	// Exit tracking. Repo mode is persistent: exitCode stays 0 and we only
	// quit on q/ctrl+c. The field exists for symmetry with Model/RunModel.
	exitCode int
	quitting bool

	// Non-fatal fetch error tracking, split per source. The GraphQL PR
	// checks fetch and the REST standalone-runs fetch run independently,
	// so a success from one source must not clear an ongoing error from
	// the other. The view renders whichever source's error is most recent.
	fetchErrChecks   error
	fetchErrChecksAt time.Time
	fetchErrRuns     error
	fetchErrRunsAt   time.Time
	fetchReceived    bool

	// Feature flags
	enableLinks bool
}

// NewRepoModel creates a new persistent repo-watch TUI model.
func NewRepoModel(
	ctx context.Context,
	token string,
	owner, repo string,
	refreshInterval time.Duration,
	styles Styles,
	enableLinks bool,
	fadeSuccess, fadeFailure time.Duration,
) RepoModel {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	return RepoModel{
		ctx:             ctx,
		token:           token,
		owner:           owner,
		repo:            repo,
		prs:             make(map[int]PRViewData),
		spinner:         s,
		startTime:       time.Now(),
		lastUpdate:      time.Now(),
		refreshInterval: refreshInterval,
		styles:          styles,
		enableLinks:     enableLinks,
		fadeSuccess:     fadeSuccess,
		fadeFailure:     fadeFailure,
	}
}

// ExitCode returns the exit code for the program. Repo mode is persistent and
// only exits on user quit, so this is always 0.
func (m RepoModel) ExitCode() int {
	return m.exitCode
}

// sortedPRNumbers returns PR numbers in ascending order for stable rendering.
func (m RepoModel) sortedPRNumbers() []int {
	nums := make([]int, 0, len(m.prs))
	for n := range m.prs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}

// fadeWindow returns the larger of the two fade durations, used to bound the
// "recently completed" REST query window for standalone runs.
func (m RepoModel) fadeWindow() time.Duration {
	if m.fadeFailure > m.fadeSuccess {
		return m.fadeFailure
	}
	return m.fadeSuccess
}

// jobDedupKey returns the canonical dedup key for a check/job: lowercase
// "headSHA|workflow|name". SHA scopes the match to the same commit, and
// (workflow, name) identifies the job. Lowercasing guards against GitHub
// returning case variants of the same workflow/job name across the GraphQL
// and REST paths. An empty HeadSHA produces a key prefixed with "|", which
// only matches other empty-SHA entries — so jobs missing a SHA never dedup
// against PR checks that do have one (and vice versa).
func jobDedupKey(headSHA, workflowName, name string) string {
	return strings.ToLower(fmt.Sprintf("%s|%s|%s", headSHA, workflowName, name))
}

// prJobKeySet builds a set of dedup keys for every check run across all
// currently-tracked PRs. Used to suppress duplicate jobs arriving via the
// standalone branch-runs path (see dedupeAndAttachExtraJobs).
func (m RepoModel) prJobKeySet() map[string]bool {
	seen := make(map[string]bool)
	for _, pr := range m.prs {
		for _, cr := range pr.CheckRuns {
			seen[jobDedupKey(pr.HeadSHA, cr.WorkflowName, cr.Name)] = true
		}
	}
	return seen
}

// prByHeadSHA returns the PR number whose HeadSHA matches, or 0 if none.
// Used to attach leftover branch-run jobs under their matching PR.
func (m RepoModel) prByHeadSHA(sha string) int {
	for prNum, pr := range m.prs {
		if pr.HeadSHA != "" && pr.HeadSHA == sha {
			return prNum
		}
	}
	return 0
}

// dedupeAndAttachExtraJobs reconciles the standalone branch-runs view against
// the PR section to remove redundant jobs (issue #331). For each visible run it:
//
//  1. Drops any job whose (HeadSHA, WorkflowName, Name) already appears in a
//     tracked PR's CheckRuns — the PR section is authoritative for those.
//  2. If the run's HeadSHA matches a tracked PR, attaches the leftover ("extra")
//     jobs (e.g. Copilot) under that PR's CheckRuns so they render in the PR
//     group rather than a separate branch section.
//  3. If the run's HeadSHA matches no PR (truly standalone commit), keeps the
//     run (with all its jobs) in the returned slice for the branch section.
//
// Completed runs that end up with zero jobs after dedup are dropped (no header
// to render); active runs are always kept so the user sees they're running.
//
// The two fetch handlers run concurrently in arbitrary order, so this runs on
// every RepoRunsUpdateMsg against whatever m.prs currently holds. If PR data
// has not yet arrived, every run is treated as standalone and shown in the
// branch section; the next tick reconciles once both sources are present.
func (m *RepoModel) dedupeAndAttachExtraJobs(visible []ghclient.BranchRunData) []ghclient.BranchRunData {
	if len(m.prs) == 0 {
		return visible
	}

	seen := m.prJobKeySet()
	// Attach extras to a copy of the matching PR's CheckRuns slice so we don't
	// alias the map value's backing array across appends. Collect attachments
	// and apply after the loop to keep the dedup key set stable during iteration.
	type attachment struct {
		prNum int
		jobs  []ghclient.CheckRunInfo
	}
	var attachments []attachment

	var standalone []ghclient.BranchRunData
	for _, run := range visible {
		var leftover []ghclient.CheckRunInfo
		for _, job := range run.Jobs {
			key := jobDedupKey(run.HeadSHA, job.WorkflowName, job.Name)
			if seen[key] {
				continue
			}
			leftover = append(leftover, job)
			// Record the extra in the seen set so we don't attach the same job
			// twice if multiple runs on the same SHA report it.
			seen[key] = true
		}

		if prNum := m.prByHeadSHA(run.HeadSHA); prNum != 0 {
			// Run shares a commit with a tracked PR. Attach leftovers under it.
			if len(leftover) > 0 {
				attachments = append(attachments, attachment{prNum: prNum, jobs: leftover})
			}
			// Drop the run from the standalone section regardless: its commit
			// is owned by a PR. If it has no leftovers, it renders nothing; if
			// active, the user still sees the PR's checks (including this run's
			// jobs that surfaced via GraphQL) and the attached extras.
			continue
		}

		// No matching PR: truly standalone. Keep the run with its (possibly
		// reduced) job list. Drop completed runs that have no jobs left.
		if len(leftover) == 0 && !isActiveBranchRun(run.Status) {
			continue
		}
		run.Jobs = leftover
		standalone = append(standalone, run)
	}

	for _, a := range attachments {
		pr := m.prs[a.prNum]
		pr.CheckRuns = append(pr.CheckRuns, a.jobs...)
		m.prs[a.prNum] = pr
	}

	return standalone
}

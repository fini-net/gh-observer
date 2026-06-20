package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// PRViewData is the per-PR view state held by RepoModel after fade-out
// filtering. HeadSHA carries the PR head commit OID so the runs handler can
// match standalone runs to a PR and dedupe/attach their jobs. CheckRuns holds
// the GraphQL-sourced checks and is replaced wholesale on every
// RepoChecksUpdateMsg. ExtraCheckRuns holds "extra" jobs (e.g. Copilot) that
// the PR GraphQL query misses but the REST branch-runs path surfaces; it is
// rebuilt from the current visible runs on every RepoRunsUpdateMsg (not
// appended to) and preserved across PR-checks polls so extras don't flicker
// out between runs ticks. The view layer merges the two for rendering.
type PRViewData struct {
	Title          string
	CheckRuns      []ghclient.CheckRunInfo
	ExtraCheckRuns []ghclient.CheckRunInfo
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
// "headSHA\x00workflow\x00name". SHA scopes the match to the same commit, and
// (workflow, name) identifies the job. Lowercasing guards against GitHub
// returning case variants of the same workflow/job name across the GraphQL
// and REST paths. A NUL byte separates the fields so a workflow or job name
// containing "|" can't collide with another split across fields. An empty
// HeadSHA produces a key prefixed with "\x00", which only matches other
// empty-SHA entries — so jobs missing a SHA never dedup against PR checks
// that do have one (and vice versa).
func jobDedupKey(headSHA, workflowName, name string) string {
	return strings.ToLower(strings.Join([]string{headSHA, workflowName, name}, "\x00"))
}

// prCheckKeySet builds a set of dedup keys for the GraphQL-sourced CheckRuns
// across all currently-tracked PRs. Used to suppress jobs arriving via the
// standalone branch-runs path whose (HeadSHA, WorkflowName, Name) already
// appear in a PR's authoritative CheckRuns (see dedupeAndAttachExtraJobs).
// Prior ExtraCheckRuns are intentionally excluded: extras are rebuilt every
// runs tick from the current visible set, so a still-present extra must be
// re-attachable rather than treated as already seen.
func (m RepoModel) prCheckKeySet() map[string]bool {
	seen := make(map[string]bool)
	for _, pr := range m.prs {
		for _, cr := range pr.CheckRuns {
			seen[jobDedupKey(pr.HeadSHA, cr.WorkflowName, cr.Name)] = true
		}
	}
	return seen
}

// prByHeadSHA returns the lowest PR number whose HeadSHA matches, or 0 if
// none. Iterating in ascending PR-number order makes the selection
// deterministic when multiple PRs share a HeadSHA (e.g. a PR and its
// backport pointing at the same commit). SHAs from GitHub's GraphQL oid
// and REST head_sha fields are lowercase hex, so a direct comparison is
// sufficient; jobDedupKey additionally lowercases for the dedup path.
func (m RepoModel) prByHeadSHA(sha string) int {
	for _, prNum := range m.sortedPRNumbers() {
		pr := m.prs[prNum]
		if pr.HeadSHA != "" && pr.HeadSHA == sha {
			return prNum
		}
	}
	return 0
}

// dedupeAndAttachExtraJobs reconciles the standalone branch-runs view against
// the PR section to remove redundant jobs (issue #331). For each visible run:
// drops jobs whose (HeadSHA, WorkflowName, Name) already appear in a tracked
// PR's CheckRuns (authoritative); if the run's HeadSHA matches a tracked PR,
// attaches the leftover ("extra") jobs under that PR's ExtraCheckRuns so they
// render in the PR group; otherwise keeps the run (with leftover jobs) as
// standalone. Completed runs with zero leftover jobs are dropped; active runs
// are always kept. ExtraCheckRuns is rebuilt (not appended to) every call, so
// extras whose run has faded out of the visible window are dropped too — they
// don't linger under the PR after their run is gone. The two fetch handlers
// run concurrently in arbitrary order; if PR data has not yet arrived, every
// run is treated as standalone and shown in the branch section, reconciling
// on the next tick once both sources are present.
func (m *RepoModel) dedupeAndAttachExtraJobs(visible []ghclient.BranchRunData) []ghclient.BranchRunData {
	if len(m.prs) == 0 {
		return visible
	}

	seen := m.prCheckKeySet()
	// Reset every tracked PR's ExtraCheckRuns up front: extras are rebuilt
	// from the current visible runs each tick, so extras whose run has faded
	// out of the window (or now produces no leftover) are dropped rather than
	// lingering under the PR. Attachments below repopulate for PRs that still
	// have a matching visible run with leftover jobs.
	for prNum, pr := range m.prs {
		if len(pr.ExtraCheckRuns) > 0 {
			pr.ExtraCheckRuns = nil
			m.prs[prNum] = pr
		}
	}
	// Collect attachments and apply after the loop to keep the dedup key set
	// stable during iteration.
	attachments := make(map[int][]ghclient.CheckRunInfo)
	var attachmentOrder []int

	var standalone []ghclient.BranchRunData
	for _, run := range visible {
		var leftover []ghclient.CheckRunInfo
		for _, job := range run.Jobs {
			key := jobDedupKey(run.HeadSHA, job.WorkflowName, job.Name)
			if seen[key] {
				continue
			}
			leftover = append(leftover, job)
			// Record the extra in the seen set so we don't attach the same
			// job twice if multiple runs on the same SHA report it.
			seen[key] = true
		}

		if prNum := m.prByHeadSHA(run.HeadSHA); prNum != 0 {
			// Run shares a commit with a tracked PR. Attach leftovers under
			// it; drop the run from the standalone section regardless (its
			// commit is owned by a PR).
			if len(leftover) > 0 {
				if _, ok := attachments[prNum]; !ok {
					attachmentOrder = append(attachmentOrder, prNum)
				}
				attachments[prNum] = append(attachments[prNum], leftover...)
			}
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

	for _, prNum := range attachmentOrder {
		pr := m.prs[prNum]
		pr.ExtraCheckRuns = attachments[prNum]
		m.prs[prNum] = pr
	}

	return standalone
}

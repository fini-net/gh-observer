package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v90/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

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
// HeadPushedTime is the time the run's head commit was pushed to GitHub
// (sourced from GraphQL pushedDate, with committedDate as a fallback and
// the REST head_commit.timestamp as a last resort when GraphQL is
// unavailable). The "Pushed Xs ago" header renders this value, so it must
// reflect the push event — not the (potentially much older) commit author
// or committer timestamp (issue #349).
type RunInfo struct {
	ID             int64
	DisplayTitle   string
	HeadSHA        string
	HeadCommitMsg  string
	HeadPushedTime *github.Timestamp
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

// commitPushedDateQuery resolves the push time of a single commit via
// GraphQL. The Actions REST API exposes only head_commit.timestamp (the
// committer date), not the push time, so a small GraphQL lookup against
// Repository.object(oid:) ... on Commit is required to obtain pushedDate
// (issue #349). committedDate is also fetched as a fallback for rare
// cases where pushedDate is absent (e.g. some tagged-release runs not
// tied to a push). RateLimit is requested for consistency with the
// codebase's other GraphQL queries so this one-shot call participates in
// the app's rate-limit accounting.
type commitPushedDateQuery struct {
	Repository struct {
		Object struct {
			Commit struct {
				PushedDate    githubv4.DateTime `graphql:"pushedDate"`
				CommittedDate githubv4.DateTime `graphql:"committedDate"`
			} `graphql:"... on Commit"`
		} `graphql:"object(oid: $oid)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
	RateLimit struct {
		Remaining int
	}
}

// fetchCommitPushedTime looks up pushedDate (with committedDate fallback)
// for the given SHA via a single GraphQL query. Returns the zero time
// when the lookup fails or returns no usable timestamp; callers are
// expected to fall back to another source rather than abort. The second
// return value is the GraphQL rate limit remaining after the call (0 on
// error or when the query is skipped), so callers can fold it into the
// app's rate-limit accounting alongside other GraphQL queries.
// The unexported helper takes a graphqlQuerier so tests can inject a mock;
// the public caller builds the client from the token via the wrapper below.
func fetchCommitPushedTimeWithClient(ctx context.Context, client graphqlQuerier, owner, repo, sha string) (time.Time, int) {
	if sha == "" {
		return time.Time{}, 0
	}

	var q commitPushedDateQuery
	variables := map[string]any{
		"owner": githubv4.String(owner),
		"repo":  githubv4.String(repo),
		"oid":   githubv4.GitObjectID(sha),
	}
	if err := client.Query(ctx, &q, variables); err != nil {
		debug.Log("commit pushedDate lookup failed", "owner", owner, "repo", repo, "sha", sha, "err", err)
		return time.Time{}, 0
	}
	rateLimitRemaining := q.RateLimit.Remaining
	debug.Log("commit pushedDate lookup", "owner", owner, "repo", repo, "sha", sha, "rate_limit_remaining", rateLimitRemaining)
	if !q.Repository.Object.Commit.PushedDate.IsZero() {
		return q.Repository.Object.Commit.PushedDate.Time, rateLimitRemaining
	}
	if !q.Repository.Object.Commit.CommittedDate.IsZero() {
		return q.Repository.Object.Commit.CommittedDate.Time, rateLimitRemaining
	}
	return time.Time{}, rateLimitRemaining
}

// fetchCommitPushedTime builds an authenticated GraphQL client from token
// and delegates to fetchCommitPushedTimeWithClient. A missing token yields
// the zero time and a 0 rate limit (callers fall back to the REST
// timestamp and keep their existing rate-limit value).
func fetchCommitPushedTime(ctx context.Context, token, owner, repo, sha string) (time.Time, int) {
	if token == "" {
		return time.Time{}, 0
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(ctx, src)
	client := githubv4.NewClient(httpClient)
	return fetchCommitPushedTimeWithClient(ctx, client, owner, repo, sha)
}

// FetchRunInfo retrieves metadata for a workflow run by its ID. The head
// commit's push time is sourced from a GraphQL pushedDate lookup (with
// committedDate fallback); if that lookup fails, it falls back to the
// REST head_commit.timestamp so the "Pushed Xs ago" header still renders.
// A non-empty token is required for the GraphQL path; without it the REST
// fallback is used directly. Callers that already hold a token should pass
// it here rather than letting this function re-derive it via GetToken()
// (which may shell out to `gh auth token`).
//
// The second return value is the GitHub API rate limit remaining after the
// GraphQL lookup (or 5000 — the REST default — when the lookup is skipped
// or fails, since the REST GetWorkflowRunByID call does not expose a
// remaining count in a way this function threads back). Callers should
// fold it into their rate-limit accounting; when the GraphQL call
// succeeds its observed value is returned, which may be lower than the
// REST-side reality and is intentionally conservative.
func FetchRunInfo(ctx context.Context, client *github.Client, token, owner, repo string, runID int64) (*RunInfo, int, error) {
	run, _, err := client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
	if err != nil {
		return nil, 5000, fmt.Errorf("failed to fetch workflow run %d: %w", runID, err)
	}

	info := &RunInfo{
		ID:         run.GetID(),
		Status:     run.GetStatus(),
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
		// REST fallback: head_commit.timestamp is the committer date, not
		// the push time. Used only if the GraphQL lookup below fails or
		// returns no timestamp (issue #349).
		if run.HeadCommit.Timestamp != nil {
			info.HeadPushedTime = run.HeadCommit.Timestamp
		}
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

	// Conservative default: matches FetchRunJobs's sentinel when no
	// GraphQL rate-limit observation is available. The GraphQL lookup
	// below replaces it with the real observed value when it succeeds.
	rateLimitRemaining := 5000

	// Best-effort GraphQL lookup of pushedDate: if it succeeds, replace
	// the REST fallback with the real push time. A failure leaves the
	// fallback in place and is logged, not surfaced — the header still
	// renders (just with a slightly older timestamp). The rate limit
	// returned by the lookup is surfaced to the caller even on timestamp
	// miss so the app's backoff accounting sees this one-shot call.
	if info.HeadSHA != "" {
		pushed, rl := fetchCommitPushedTime(ctx, token, owner, repo, info.HeadSHA)
		if rl > 0 {
			rateLimitRemaining = rl
		}
		if !pushed.IsZero() {
			info.HeadPushedTime = &github.Timestamp{Time: pushed}
		}
	}

	debug.Log("fetch run info", "run_id", runID, "name", info.DisplayTitle, "status", info.Status, "rate_limit_remaining", rateLimitRemaining)

	return info, rateLimitRemaining, nil
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
			DetailsURL:    job.HTMLURL,
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

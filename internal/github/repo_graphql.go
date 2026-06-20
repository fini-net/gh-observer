package github

import (
	"context"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// maxPRsPerQuery caps the number of open PRs fetched per repo query.
// GitHub's GraphQL server-side query timeout is ~10s. Fetching check rollups
// (contexts(first: 100)) for each PR scales linearly: prLimit=50 504s on
// high-traffic repos like grafana/grafana (137 workflows, thousands of open
// PRs), while prLimit=10 returns in ~6s even for grafana. 10 most-recently-
// updated PRs is a useful persistent overview; the fade-out window means
// older completed checks disappear from the display soon anyway.
const maxPRsPerQuery = 10

// PRCheckData holds check run data for a single PR in repo mode.
// HeadSHA is the PR head commit OID, used to dedupe standalone branch runs
// (see RepoModel.dedupeAndAttachExtraJobs) against the PR section.
type PRCheckData struct {
	Number         int
	Title          string
	CheckRuns      []CheckRunInfo
	HeadCommitTime time.Time
	HeadSHA        string
}

// repoContextNode is the union type for StatusCheckRollup contexts in the
// repo-mode query. It is a trimmed copy of contextNode from graphql.go that
// OMITS the annotations(first: 5) field. Annotations are the single most
// expensive part of the query (5 nodes × ~100 contexts × N PRs) and push the
// query over GitHub's GraphQL "Resource limits for this query exceeded"
// threshold on high-traffic repos like grafana/grafana at prLimit=10. Repo
// mode is a persistent overview and never renders annotations (only single-PR
// mode's renderErrorBox does), so dropping them here is safe and makes the
// query 10/10 reliable instead of 7/10 borderline-504.
type repoContextNode struct {
	Typename        string `graphql:"__typename"`
	CheckRunContext struct {
		Name        string
		Summary     string
		Status      string
		Conclusion  string
		StartedAt   githubv4.DateTime
		CompletedAt githubv4.DateTime
		DetailsURL  string `graphql:"detailsUrl"`
		CheckSuite  struct {
			WorkflowRun struct {
				DatabaseID BigInt `graphql:"databaseId"`
				Workflow   struct {
					DatabaseID BigInt `graphql:"databaseId"`
					Name       string
				}
			}
			App struct {
				Name string
				Slug string
			}
		}
	} `graphql:"... on CheckRun"`
	StatusContext struct {
		Context     string
		Description string
		State       string
		TargetURL   string `graphql:"targetUrl"`
	} `graphql:"... on StatusContext"`
}

// repoPRQuery fetches open PRs with their check rollup in a single query.
// Uses repoContextNode (no annotations) to stay under GitHub's query cost limit.
type repoPRQuery struct {
	Repository struct {
		PullRequests struct {
			Nodes []struct {
				Number  int
				Title   string
				Commits struct {
					Nodes []struct {
					Commit struct {
						PushedDate    githubv4.DateTime `graphql:"pushedDate"`
						CommittedDate githubv4.DateTime `graphql:"committedDate"`
						OID           githubv4.String  `graphql:"oid"`
							StatusCheckRollup struct {
								Contexts struct {
									Nodes    []repoContextNode
									PageInfo struct {
										HasNextPage bool
										EndCursor   githubv4.String
									}
								} `graphql:"contexts(first: 100)"`
							}
						}
					}
				} `graphql:"commits(last: 1)"`
			}
		} `graphql:"pullRequests(first: $prLimit, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC})"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
	RateLimit struct {
		Remaining int
	}
}

// FetchRepoCheckRunsGraphQL fetches check runs for all open PRs in a repo
// using a single batched GraphQL query. Returns a map of PR number to PRCheckData
// and the remaining rate limit.
//
// Note: each PR's status contexts are capped at 100 (no cursor pagination).
// PRs with more than 100 status contexts will show an incomplete set; a future
// enhancement can fall back to per-PR pagination for PRs that hit the cap.
//
// The query deliberately omits the annotations(first: 5) field to stay under
// GitHub's GraphQL query cost limit on high-traffic repos. Repo mode never
// renders inline error annotations (only single-PR mode does), so CheckRunInfo
// entries from this path have an empty Annotations slice.
func FetchRepoCheckRunsGraphQL(ctx context.Context, token, owner, repo string) (map[int]PRCheckData, int, error) {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(ctx, src)
	client := githubv4.NewClient(httpClient)
	return fetchRepoCheckRunsGraphQL(ctx, client, owner, repo)
}

func fetchRepoCheckRunsGraphQL(ctx context.Context, client graphqlQuerier, owner, repo string) (map[int]PRCheckData, int, error) {
	var query repoPRQuery
	variables := map[string]any{
		"owner":   githubv4.String(owner),
		"repo":    githubv4.String(repo),
		"prLimit": githubv4.Int(maxPRsPerQuery),
	}

	err := client.Query(ctx, &query, variables)
	if err != nil {
		debug.Log("repo graphql query failed", "owner", owner, "repo", repo, "err", err)
		return nil, 5000, err
	}

	debug.Log("repo graphql query success", "owner", owner, "repo", repo,
		"pr_count", len(query.Repository.PullRequests.Nodes),
		"rate_limit_remaining", query.RateLimit.Remaining)

	rateLimitRemaining := query.RateLimit.Remaining

	result := make(map[int]PRCheckData)
	for _, pr := range query.Repository.PullRequests.Nodes {
		if len(pr.Commits.Nodes) == 0 {
			continue
		}

		commit := pr.Commits.Nodes[0].Commit
		var headCommitTime time.Time
		if !commit.PushedDate.IsZero() {
			headCommitTime = commit.PushedDate.Time
		} else if !commit.CommittedDate.IsZero() {
			headCommitTime = commit.CommittedDate.Time
		}

		checkRuns := repoContextNodesToCheckRuns(commit.StatusCheckRollup.Contexts.Nodes)

		if len(checkRuns) == 0 && headCommitTime.IsZero() {
			continue
		}

		result[pr.Number] = PRCheckData{
			Number:         pr.Number,
			Title:          pr.Title,
			CheckRuns:      checkRuns,
			HeadCommitTime: headCommitTime,
			HeadSHA:        string(commit.OID),
		}
	}

	return result, rateLimitRemaining, nil
}

// repoContextNodesToCheckRuns converts repoContextNode slice to CheckRunInfo.
// It is a trimmed copy of contextNodesToCheckRuns from graphql.go that always
// produces an empty Annotations slice (the repo query does not fetch them).
func repoContextNodesToCheckRuns(nodes []repoContextNode) []CheckRunInfo {
	var checkRuns []CheckRunInfo

	for _, node := range nodes {
		if node.Typename == "StatusContext" {
			statusContext := node.StatusContext
			state := strings.ToLower(statusContext.State)

			var status, conclusion string
			switch state {
			case "success":
				status = "completed"
				conclusion = "success"
			case "error", "failure":
				status = "completed"
				conclusion = "failure"
			case "pending":
				status = "queued"
				conclusion = ""
			default:
				status = "queued"
				conclusion = ""
			}

			checkRuns = append(checkRuns, CheckRunInfo{
				Name:       statusContext.Context,
				Summary:    statusContext.Description,
				Status:     status,
				Conclusion: conclusion,
				DetailsURL: statusContext.TargetURL,
			})
			continue
		}

		if node.Typename != "CheckRun" {
			continue
		}

		checkRun := node.CheckRunContext
		workflowName := checkRun.CheckSuite.WorkflowRun.Workflow.Name
		appName := checkRun.CheckSuite.App.Name
		workflowRunID := int64(checkRun.CheckSuite.WorkflowRun.DatabaseID)
		workflowID := int64(checkRun.CheckSuite.WorkflowRun.Workflow.DatabaseID)

		var startedAt, completedAt *time.Time
		if !checkRun.StartedAt.IsZero() {
			t := checkRun.StartedAt.Time
			startedAt = &t
		}
		if !checkRun.CompletedAt.IsZero() {
			t := checkRun.CompletedAt.Time
			completedAt = &t
		}

		checkRuns = append(checkRuns, CheckRunInfo{
			Name:          checkRun.Name,
			WorkflowName:  workflowName,
			AppName:       appName,
			Summary:       checkRun.Summary,
			Status:        strings.ToLower(checkRun.Status),
			Conclusion:    strings.ToLower(checkRun.Conclusion),
			StartedAt:     startedAt,
			CompletedAt:   completedAt,
			DetailsURL:    checkRun.DetailsURL,
			WorkflowRunID: workflowRunID,
			WorkflowID:    workflowID,
		})
	}

	return checkRuns
}
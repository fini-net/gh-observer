package github

import (
	"context"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// maxPRsPerQuery caps the number of open PRs fetched per repo query.
// 50 matches GitHub's typical "first" page size for pullRequests and keeps
// the batched query cost reasonable.
const maxPRsPerQuery = 50

// PRCheckData holds check run data for a single PR in repo mode.
type PRCheckData struct {
	Number         int
	Title          string
	CheckRuns      []CheckRunInfo
	HeadCommitTime time.Time
}

// repoPRQuery fetches open PRs with their check rollup in a single query.
// Reuses contextNode from graphql.go so contextNodesToCheckRuns works unchanged.
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
							StatusCheckRollup struct {
								Contexts struct {
									Nodes    []contextNode
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

		checkRuns := contextNodesToCheckRuns(commit.StatusCheckRollup.Contexts.Nodes)

		if len(checkRuns) == 0 && headCommitTime.IsZero() {
			continue
		}

		result[pr.Number] = PRCheckData{
			Number:         pr.Number,
			Title:          pr.Title,
			CheckRuns:      checkRuns,
			HeadCommitTime: headCommitTime,
		}
	}

	return result, rateLimitRemaining, nil
}
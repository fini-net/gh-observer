package github

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// CheckRunInfo contains enriched check run data with workflow name
type CheckRunInfo struct {
	Name         string
	WorkflowName string
	Status       string
	Conclusion   string
	StartedAt    *time.Time
	CompletedAt  *time.Time
	DetailsURL   string
}

// GraphQL query structure matching gh pr checks
type pullRequestQuery struct {
	Repository struct {
		PullRequest struct {
			Commits struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup struct {
							Contexts struct {
								Nodes []struct {
									Typename        string `graphql:"__typename"`
									CheckRunContext struct {
										Name       string
										Status     string
										Conclusion string
										StartedAt  githubv4.DateTime
										CompletedAt githubv4.DateTime
										DetailsURL string `graphql:"detailsUrl"`
										CheckSuite struct {
											WorkflowRun struct {
												Workflow struct {
													Name string
												}
											}
										}
									} `graphql:"... on CheckRun"`
								}
							} `graphql:"contexts(first: 100)"`
						}
					}
				}
			} `graphql:"commits(last: 1)"`
		} `graphql:"pullRequest(number: $prNumber)"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
	RateLimit struct {
		Remaining int
	}
}

// FetchCheckRunsGraphQL fetches check runs with workflow names using GraphQL
func FetchCheckRunsGraphQL(ctx context.Context, token, owner, repo string, prNumber int) ([]CheckRunInfo, int, error) {
	// Create GraphQL client
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpClient := oauth2.NewClient(ctx, src)
	client := githubv4.NewClient(httpClient)

	// Execute query
	var query pullRequestQuery
	variables := map[string]interface{}{
		"owner":    githubv4.String(owner),
		"repo":     githubv4.String(repo),
		"prNumber": githubv4.Int(prNumber),
	}

	err := client.Query(ctx, &query, variables)
	if err != nil {
		return nil, 0, err
	}

	// Extract check runs
	var checkRuns []CheckRunInfo

	if len(query.Repository.PullRequest.Commits.Nodes) > 0 {
		commit := query.Repository.PullRequest.Commits.Nodes[0]
		contexts := commit.Commit.StatusCheckRollup.Contexts.Nodes

		for _, context := range contexts {
			// Only process CheckRun types (not StatusContext)
			if context.Typename != "CheckRun" {
				continue
			}

			checkRun := context.CheckRunContext
			workflowName := ""

			// Extract workflow name if available
			if checkRun.CheckSuite.WorkflowRun.Workflow.Name != "" {
				workflowName = checkRun.CheckSuite.WorkflowRun.Workflow.Name
			}

			var startedAt, completedAt *time.Time
			if !checkRun.StartedAt.Time.IsZero() {
				t := checkRun.StartedAt.Time
				startedAt = &t
			}
			if !checkRun.CompletedAt.Time.IsZero() {
				t := checkRun.CompletedAt.Time
				completedAt = &t
			}

			checkRuns = append(checkRuns, CheckRunInfo{
				Name:         checkRun.Name,
				WorkflowName: workflowName,
				Status:       strings.ToLower(checkRun.Status),
				Conclusion:   strings.ToLower(checkRun.Conclusion),
				StartedAt:    startedAt,
				CompletedAt:  completedAt,
				DetailsURL:   checkRun.DetailsURL,
			})
		}
	}

	return checkRuns, query.RateLimit.Remaining, nil
}

package github

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Annotation represents a check run annotation (error/warning)
type Annotation struct {
	Message         string
	Path            string
	StartLine       int
	Title           string
	AnnotationLevel string
}

// CheckRunInfo contains enriched check run data with workflow name
type CheckRunInfo struct {
	Name         string
	WorkflowName string
	Summary      string
	Status       string
	Conclusion   string
	StartedAt    *time.Time
	CompletedAt  *time.Time
	DetailsURL   string
	Annotations  []Annotation
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
										Name        string
										Summary     string
										Status      string
										Conclusion  string
										StartedAt   githubv4.DateTime
										CompletedAt githubv4.DateTime
										DetailsURL  string `graphql:"detailsUrl"`
										Annotations struct {
											Nodes []struct {
												Message         string
												Path            string
												Title           string
												AnnotationLevel string
												Location        struct {
													Start struct {
														Line int
													} `graphql:"start"`
												} `graphql:"location"`
											}
										} `graphql:"annotations(first: 5)"`
										CheckSuite struct {
											WorkflowRun struct {
												Workflow struct {
													Name string
												}
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
			if context.Typename == "StatusContext" {
				statusContext := context.StatusContext
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

			if context.Typename != "CheckRun" {
				continue
			}

			checkRun := context.CheckRunContext
			workflowName := ""

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

			var annotations []Annotation
			for _, ann := range checkRun.Annotations.Nodes {
				annotations = append(annotations, Annotation{
					Message:         ann.Message,
					Path:            ann.Path,
					StartLine:       ann.Location.Start.Line,
					Title:           ann.Title,
					AnnotationLevel: strings.ToLower(ann.AnnotationLevel),
				})
			}

			checkRuns = append(checkRuns, CheckRunInfo{
				Name:         checkRun.Name,
				WorkflowName: workflowName,
				Summary:      checkRun.Summary,
				Status:       strings.ToLower(checkRun.Status),
				Conclusion:   strings.ToLower(checkRun.Conclusion),
				StartedAt:    startedAt,
				CompletedAt:  completedAt,
				DetailsURL:   checkRun.DetailsURL,
				Annotations:  annotations,
			})
		}
	}

	return checkRuns, query.RateLimit.Remaining, nil
}

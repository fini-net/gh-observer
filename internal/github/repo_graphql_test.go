package github

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

// makeRepoCheckRunNode builds a repoContextNode with a CheckRun typename and
// the given fields. Mirrors makeCheckRunNode but for the repo-mode struct
// (which has no Annotations field).
func makeRepoCheckRunNode(name, status, conclusion, workflowName, appName string, startedAt, completedAt githubv4.DateTime, detailsURL string, workflowRunID, workflowID int64) repoContextNode {
	return repoContextNode{
		Typename: "CheckRun",
		CheckRunContext: struct {
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
		}{
			Name:        name,
			Status:      status,
			Conclusion:  conclusion,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
			DetailsURL:  detailsURL,
			CheckSuite: struct {
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
			}{
				WorkflowRun: struct {
					DatabaseID BigInt `graphql:"databaseId"`
					Workflow   struct {
						DatabaseID BigInt `graphql:"databaseId"`
						Name       string
					}
				}{
					DatabaseID: BigInt(workflowRunID),
					Workflow: struct {
						DatabaseID BigInt `graphql:"databaseId"`
						Name       string
					}{
						DatabaseID: BigInt(workflowID),
						Name:       workflowName,
					},
				},
				App: struct {
					Name string
					Slug string
				}{
					Name: appName,
				},
			},
		},
	}
}

func TestRepoContextNodesToCheckRuns(t *testing.T) {
	startedAt := githubv4.DateTime{}
	startedAt.Time = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	completedAt := githubv4.DateTime{}
	completedAt.Time = time.Date(2024, 1, 15, 10, 35, 0, 0, time.UTC)

	tests := []struct {
		name              string
		nodes             []repoContextNode
		wantLen           int
		wantNames         []string
		wantWorkflowNames []string
		wantAppNames      []string
		wantAnnotations   int // all entries should have this many annotations (always 0 for repo mode)
	}{
		{
			name:  "empty nodes returns nil",
			nodes: []repoContextNode{},
			wantLen: 0,
		},
		{
			name: "StatusContext success maps to completed/success",
			nodes: []repoContextNode{
				{
					Typename: "StatusContext",
					StatusContext: struct {
						Context     string
						Description string
						State       string
						TargetURL   string `graphql:"targetUrl"`
					}{
						Context:   "ci/travis",
						State:     "success",
						TargetURL: "https://travis-ci.org/owner/repo/builds/1",
					},
				},
			},
			wantLen:   1,
			wantNames: []string{"ci/travis"},
		},
		{
			name: "StatusContext pending maps to queued",
			nodes: []repoContextNode{
				{
					Typename: "StatusContext",
					StatusContext: struct {
						Context     string
						Description string
						State       string
						TargetURL   string `graphql:"targetUrl"`
					}{
						Context: "ci/waiting",
						State:   "pending",
					},
				},
			},
			wantLen:   1,
			wantNames: []string{"ci/waiting"},
		},
		{
			name: "CheckRun with workflow name and timestamps",
			nodes: []repoContextNode{
				makeRepoCheckRunNode("lint", "COMPLETED", "SUCCESS", "CI", "GitHub Actions", startedAt, completedAt, "https://github.com/owner/repo/actions/runs/100/job/200", 100, 10),
			},
			wantLen:           1,
			wantNames:         []string{"lint"},
			wantWorkflowNames: []string{"CI"},
			wantAppNames:      []string{"GitHub Actions"},
		},
		{
			name: "CheckRun without workflow name",
			nodes: []repoContextNode{
				makeRepoCheckRunNode("legacy-check", "COMPLETED", "FAILURE", "", "", githubv4.DateTime{}, githubv4.DateTime{}, "", 0, 0),
			},
			wantLen:           1,
			wantNames:         []string{"legacy-check"},
			wantWorkflowNames: []string{""},
		},
		{
			name: "unknown typename is skipped",
			nodes: []repoContextNode{
				{Typename: "SomeOtherType"},
				makeRepoCheckRunNode("valid-check", "IN_PROGRESS", "", "", "", githubv4.DateTime{}, githubv4.DateTime{}, "", 0, 0),
			},
			wantLen:   1,
			wantNames: []string{"valid-check"},
		},
		{
			name: "mixed StatusContext and CheckRun nodes",
			nodes: []repoContextNode{
				{
					Typename: "StatusContext",
					StatusContext: struct {
						Context     string
						Description string
						State       string
						TargetURL   string `graphql:"targetUrl"`
					}{
						Context: "ci/travis",
						State:   "success",
					},
				},
				makeRepoCheckRunNode("test", "QUEUED", "", "", "", githubv4.DateTime{}, githubv4.DateTime{}, "", 0, 0),
			},
			wantLen:   2,
			wantNames: []string{"ci/travis", "test"},
		},
		{
			name: "CheckRun with zero timestamps produces nil time",
			nodes: []repoContextNode{
				makeRepoCheckRunNode("build", "QUEUED", "", "CI", "", githubv4.DateTime{}, githubv4.DateTime{}, "", 0, 0),
			},
			wantLen:   1,
			wantNames: []string{"build"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := repoContextNodesToCheckRuns(tt.nodes)

			if len(runs) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(runs), tt.wantLen)
			}

			for i, run := range runs {
				if i < len(tt.wantNames) && run.Name != tt.wantNames[i] {
					t.Errorf("Name[%d] = %q, want %q", i, run.Name, tt.wantNames[i])
				}
				if i < len(tt.wantWorkflowNames) && run.WorkflowName != tt.wantWorkflowNames[i] {
					t.Errorf("WorkflowName[%d] = %q, want %q", i, run.WorkflowName, tt.wantWorkflowNames[i])
				}
				if i < len(tt.wantAppNames) && run.AppName != tt.wantAppNames[i] {
					t.Errorf("AppName[%d] = %q, want %q", i, run.AppName, tt.wantAppNames[i])
				}
				// Repo mode never fetches annotations; verify always empty.
				if len(run.Annotations) != 0 {
					t.Errorf("Annotations[%d] = %v, want empty (repo mode drops annotations)", i, run.Annotations)
				}
			}
		})
	}
}

// repoMockQuerier is a mock graphqlQuerier for repo-mode tests. Unlike
// mockQuerier in graphql_test.go (hardwired to *pullRequestQuery), this one
// serves *repoPRQuery responses.
type repoMockQuerier struct {
	responses []*repoPRQuery
	errs      []error
	calls     int
}

func (m *repoMockQuerier) Query(_ context.Context, q interface{}, _ map[string]interface{}) error {
	if m.calls >= len(m.responses) {
		return fmt.Errorf("unexpected repo query call #%d", m.calls+1)
	}
	var err error
	if m.calls < len(m.errs) {
		err = m.errs[m.calls]
	}
	resp := m.responses[m.calls]
	m.calls++
	if err != nil {
		return err
	}
	target := q.(*repoPRQuery)
	*target = *resp
	return nil
}

// makeRepoPRQuery builds a *repoPRQuery with one PR carrying the given head SHA
// and a single CheckRun context node.
func makeRepoPRQuery(prNumber int, title, headSHA, checkName, workflowName string, rateLimit int) *repoPRQuery {
	startedAt := githubv4.DateTime{}
	startedAt.Time = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	q := &repoPRQuery{}
	q.RateLimit.Remaining = rateLimit

	pr := struct {
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
	}{
		Number: prNumber,
		Title:  title,
	}
	pr.Commits.Nodes = append(pr.Commits.Nodes, struct {
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
	}{
		Commit: struct {
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
		}{
			OID: githubv4.String(headSHA),
			StatusCheckRollup: struct {
				Contexts struct {
					Nodes    []repoContextNode
					PageInfo struct {
						HasNextPage bool
						EndCursor   githubv4.String
					}
				} `graphql:"contexts(first: 100)"`
			}{
				Contexts: struct {
					Nodes    []repoContextNode
					PageInfo struct {
						HasNextPage bool
						EndCursor   githubv4.String
					}
				}{
					Nodes: []repoContextNode{
						makeRepoCheckRunNode(checkName, "IN_PROGRESS", "", workflowName, "GitHub Actions", startedAt, githubv4.DateTime{}, "https://example.com", 100, 10),
					},
				},
			},
		},
	})
	q.Repository.PullRequests.Nodes = append(q.Repository.PullRequests.Nodes, pr)
	return q
}

// TestFetchRepoCheckRunsGraphQLHeadSHA verifies that the GraphQL oid field
// is requested and propagated into PRCheckData.HeadSHA — the foundation of
// the issue #331 SHA-based dedup.
func TestFetchRepoCheckRunsGraphQLHeadSHA(t *testing.T) {
	mock := &repoMockQuerier{
		responses: []*repoPRQuery{
			makeRepoPRQuery(7, "Add feature", "abc123def456", "build", "CI", 4900),
		},
	}

	prs, rateLimit, err := fetchRepoCheckRunsGraphQL(context.Background(), mock, "owner", "repo")
	if err != nil {
		t.Fatalf("fetchRepoCheckRunsGraphQL error: %v", err)
	}
	if rateLimit != 4900 {
		t.Errorf("rateLimit = %d, want 4900", rateLimit)
	}
	if len(prs) != 1 {
		t.Fatalf("prs = %d, want 1", len(prs))
	}
	pr, ok := prs[7]
	if !ok {
		t.Fatal("PR #7 missing from result")
	}
	if pr.HeadSHA != "abc123def456" {
		t.Errorf("pr.HeadSHA = %q, want %q (oid must propagate from GraphQL)", pr.HeadSHA, "abc123def456")
	}
	if len(pr.CheckRuns) != 1 {
		t.Fatalf("pr.CheckRuns = %d, want 1", len(pr.CheckRuns))
	}
	if pr.CheckRuns[0].Name != "build" {
		t.Errorf("check name = %q, want %q", pr.CheckRuns[0].Name, "build")
	}
}

// TestFetchRepoCheckRunsGraphQLEmptySHA verifies a PR whose commit OID is empty
// propagates an empty HeadSHA rather than erroring.
func TestFetchRepoCheckRunsGraphQLEmptySHA(t *testing.T) {
	mock := &repoMockQuerier{
		responses: []*repoPRQuery{
			makeRepoPRQuery(1, "No SHA", "", "lint", "CI", 4900),
		},
	}

	prs, _, err := fetchRepoCheckRunsGraphQL(context.Background(), mock, "owner", "repo")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, ok := prs[1]; !ok {
		t.Fatal("PR #1 missing")
	}
	if prs[1].HeadSHA != "" {
		t.Errorf("HeadSHA = %q, want empty", prs[1].HeadSHA)
	}
}
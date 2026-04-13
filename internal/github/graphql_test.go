package github

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

func TestContextNodesToCheckRuns(t *testing.T) {
	startedAt := githubv4.DateTime{}
	startedAt.Time = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	completedAt := githubv4.DateTime{}
	completedAt.Time = time.Date(2024, 1, 15, 10, 35, 0, 0, time.UTC)

	tests := []struct {
		name     string
		nodes    []contextNode
		wantLen  int
		wantName []string
	}{
		{
			name:     "empty nodes returns nil",
			nodes:    []contextNode{},
			wantLen:  0,
			wantName: nil,
		},
		{
			name: "StatusContext success maps to completed/success",
			nodes: []contextNode{
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
			wantLen:  1,
			wantName: []string{"ci/travis"},
		},
		{
			name: "StatusContext failure maps to completed/failure",
			nodes: []contextNode{
				{
					Typename: "StatusContext",
					StatusContext: struct {
						Context     string
						Description string
						State       string
						TargetURL   string `graphql:"targetUrl"`
					}{
						Context:   "ci/jenkins",
						State:     "failure",
						TargetURL: "https://jenkins.example.com/1",
					},
				},
			},
			wantLen:  1,
			wantName: []string{"ci/jenkins"},
		},
		{
			name: "StatusContext error maps to completed/failure",
			nodes: []contextNode{
				{
					Typename: "StatusContext",
					StatusContext: struct {
						Context     string
						Description string
						State       string
						TargetURL   string `graphql:"targetUrl"`
					}{
						Context:   "ci/circle",
						State:     "error",
						TargetURL: "https://circleci.com/1",
					},
				},
			},
			wantLen:  1,
			wantName: []string{"ci/circle"},
		},
		{
			name: "StatusContext pending maps to queued",
			nodes: []contextNode{
				{
					Typename: "StatusContext",
					StatusContext: struct {
						Context     string
						Description string
						State       string
						TargetURL   string `graphql:"targetUrl"`
					}{
						Context: "ci/pending",
						State:   "pending",
					},
				},
			},
			wantLen:  1,
			wantName: []string{"ci/pending"},
		},
		{
			name: "CheckRun with workflow name and timestamps",
			nodes: []contextNode{
				{
					Typename: "CheckRun",
					CheckRunContext: struct {
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
					}{
						Name:        "lint",
						Status:      "COMPLETED",
						Conclusion:  "SUCCESS",
						StartedAt:   startedAt,
						CompletedAt: completedAt,
						DetailsURL:  "https://github.com/owner/repo/actions/runs/100/job/200",
						CheckSuite: struct {
							WorkflowRun struct {
								Workflow struct {
									Name string
								}
							}
						}{
							WorkflowRun: struct {
								Workflow struct {
									Name string
								}
							}{
								Workflow: struct{ Name string }{Name: "CI"},
							},
						},
					},
				},
			},
			wantLen:  1,
			wantName: []string{"lint"},
		},
		{
			name: "CheckRun without workflow name",
			nodes: []contextNode{
				{
					Typename: "CheckRun",
					CheckRunContext: struct {
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
					}{
						Name:       "legacy-check",
						Status:     "COMPLETED",
						Conclusion: "FAILURE",
					},
				},
			},
			wantLen:  1,
			wantName: []string{"legacy-check"},
		},
		{
			name: "unknown typename is skipped",
			nodes: []contextNode{
				{
					Typename: "SomeOtherType",
				},
				{
					Typename: "CheckRun",
					CheckRunContext: struct {
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
					}{
						Name:       "valid-check",
						Status:     "IN_PROGRESS",
						Conclusion: "",
					},
				},
			},
			wantLen:  1,
			wantName: []string{"valid-check"},
		},
		{
			name: "mixed StatusContext and CheckRun nodes",
			nodes: []contextNode{
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
				{
					Typename: "CheckRun",
					CheckRunContext: struct {
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
					}{
						Name:       "test",
						Status:     "QUEUED",
						Conclusion: "",
					},
				},
			},
			wantLen:  2,
			wantName: []string{"ci/travis", "test"},
		},
		{
			name: "CheckRun with annotations",
			nodes: []contextNode{
				{
					Typename: "CheckRun",
					CheckRunContext: struct {
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
					}{
						Name:       "lint",
						Status:     "COMPLETED",
						Conclusion: "FAILURE",
						Annotations: struct {
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
						}{
							Nodes: []struct {
								Message         string
								Path            string
								Title           string
								AnnotationLevel string
								Location        struct {
									Start struct {
										Line int
									} `graphql:"start"`
								} `graphql:"location"`
							}{
								{
									Message:         "unused variable",
									Path:            "main.go",
									Title:           "go vet",
									AnnotationLevel: "WARNING",
									Location: struct {
										Start struct{ Line int } `graphql:"start"`
									}{
										Start: struct{ Line int }{Line: 42},
									},
								},
							},
						},
						CheckSuite: struct {
							WorkflowRun struct {
								Workflow struct {
									Name string
								}
							}
						}{
							WorkflowRun: struct {
								Workflow struct {
									Name string
								}
							}{
								Workflow: struct{ Name string }{Name: "CI"},
							},
						},
					},
				},
			},
			wantLen:  1,
			wantName: []string{"lint"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextNodesToCheckRuns(tt.nodes)
			if len(got) != tt.wantLen {
				t.Errorf("contextNodesToCheckRuns() got %d results, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen == 0 && got != nil {
				t.Errorf("contextNodesToCheckRuns() expected nil for empty result, got %v", got)
			}
			if tt.wantName != nil {
				for i, name := range tt.wantName {
					if i >= len(got) {
						break
					}
					if got[i].Name != name {
						t.Errorf("contextNodesToCheckRuns()[%d].Name = %q, want %q", i, got[i].Name, name)
					}
				}
			}
		})
	}
}

func TestContextNodesToCheckRuns_StatusMapping(t *testing.T) {
	nodes := []contextNode{
		{
			Typename: "StatusContext",
			StatusContext: struct {
				Context     string
				Description string
				State       string
				TargetURL   string `graphql:"targetUrl"`
			}{
				Context: "ci/success",
				State:   "success",
			},
		},
		{
			Typename: "StatusContext",
			StatusContext: struct {
				Context     string
				Description string
				State       string
				TargetURL   string `graphql:"targetUrl"`
			}{
				Context: "ci/failure",
				State:   "failure",
			},
		},
		{
			Typename: "StatusContext",
			StatusContext: struct {
				Context     string
				Description string
				State       string
				TargetURL   string `graphql:"targetUrl"`
			}{
				Context: "ci/error",
				State:   "error",
			},
		},
		{
			Typename: "StatusContext",
			StatusContext: struct {
				Context     string
				Description string
				State       string
				TargetURL   string `graphql:"targetUrl"`
			}{
				Context: "ci/pending",
				State:   "pending",
			},
		},
		{
			Typename: "StatusContext",
			StatusContext: struct {
				Context     string
				Description string
				State       string
				TargetURL   string `graphql:"targetUrl"`
			}{
				Context: "ci/unknown",
				State:   "unknown_state",
			},
		},
	}

	got := contextNodesToCheckRuns(nodes)

	if len(got) != 5 {
		t.Fatalf("expected 5 results, got %d", len(got))
	}

	checkMapping := []struct {
		idx        int
		name       string
		status     string
		conclusion string
	}{
		{0, "ci/success", "completed", "success"},
		{1, "ci/failure", "completed", "failure"},
		{2, "ci/error", "completed", "failure"},
		{3, "ci/pending", "queued", ""},
		{4, "ci/unknown", "queued", ""},
	}

	for _, m := range checkMapping {
		if got[m.idx].Name != m.name {
			t.Errorf("got[%d].Name = %q, want %q", m.idx, got[m.idx].Name, m.name)
		}
		if got[m.idx].Status != m.status {
			t.Errorf("got[%d].Status = %q, want %q", m.idx, got[m.idx].Status, m.status)
		}
		if got[m.idx].Conclusion != m.conclusion {
			t.Errorf("got[%d].Conclusion = %q, want %q", m.idx, got[m.idx].Conclusion, m.conclusion)
		}
	}
}

func TestContextNodesToCheckRuns_CheckRunFields(t *testing.T) {
	startedAt := githubv4.DateTime{}
	startedAt.Time = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	completedAt := githubv4.DateTime{}
	completedAt.Time = time.Date(2024, 3, 1, 12, 5, 0, 0, time.UTC)

	nodes := []contextNode{
		{
			Typename: "CheckRun",
			CheckRunContext: struct {
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
			}{
				Name:        "build / compile",
				Summary:     "Build passed",
				Status:      "completed",
				Conclusion:  "success",
				StartedAt:   startedAt,
				CompletedAt: completedAt,
				DetailsURL:  "https://github.com/owner/repo/actions/runs/42/job/99",
				CheckSuite: struct {
					WorkflowRun struct {
						Workflow struct {
							Name string
						}
					}
				}{
					WorkflowRun: struct {
						Workflow struct {
							Name string
						}
					}{
						Workflow: struct{ Name string }{Name: "CI Pipeline"},
					},
				},
			},
		},
	}

	got := contextNodesToCheckRuns(nodes)

	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}

	cr := got[0]
	if cr.Name != "build / compile" {
		t.Errorf("Name = %q, want %q", cr.Name, "build / compile")
	}
	if cr.WorkflowName != "CI Pipeline" {
		t.Errorf("WorkflowName = %q, want %q", cr.WorkflowName, "CI Pipeline")
	}
	if cr.Summary != "Build passed" {
		t.Errorf("Summary = %q, want %q", cr.Summary, "Build passed")
	}
	if cr.Status != "completed" {
		t.Errorf("Status = %q, want %q", cr.Status, "completed")
	}
	if cr.Conclusion != "success" {
		t.Errorf("Conclusion = %q, want %q", cr.Conclusion, "success")
	}
	if cr.StartedAt == nil || !cr.StartedAt.Equal(startedAt.Time) {
		t.Errorf("StartedAt = %v, want %v", cr.StartedAt, startedAt.Time)
	}
	if cr.CompletedAt == nil || !cr.CompletedAt.Equal(completedAt.Time) {
		t.Errorf("CompletedAt = %v, want %v", cr.CompletedAt, completedAt.Time)
	}
	if cr.DetailsURL != "https://github.com/owner/repo/actions/runs/42/job/99" {
		t.Errorf("DetailsURL = %q, want %q", cr.DetailsURL, "https://github.com/owner/repo/actions/runs/42/job/99")
	}
}

func TestContextNodesToCheckRuns_ZeroTimestamps(t *testing.T) {
	nodes := []contextNode{
		{
			Typename: "CheckRun",
			CheckRunContext: struct {
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
			}{
				Name:       "queued-check",
				Status:     "queued",
				Conclusion: "",
			},
		},
	}

	got := contextNodesToCheckRuns(nodes)

	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}

	if got[0].StartedAt != nil {
		t.Errorf("StartedAt should be nil for zero timestamp, got %v", got[0].StartedAt)
	}
	if got[0].CompletedAt != nil {
		t.Errorf("CompletedAt should be nil for zero timestamp, got %v", got[0].CompletedAt)
	}
}

type mockQuerier struct {
	responses []mockResponse
	callCount int
}

type mockResponse struct {
	query *pullRequestQuery
	err   error
}

func (m *mockQuerier) Query(_ context.Context, q interface{}, _ map[string]interface{}) error {
	if m.callCount >= len(m.responses) {
		return fmt.Errorf("unexpected query call #%d", m.callCount+1)
	}
	resp := m.responses[m.callCount]
	m.callCount++
	if resp.err != nil {
		return resp.err
	}
	target := q.(*pullRequestQuery)
	*target = *resp.query
	return nil
}

func makeContextNodeCheckRun(name, status, conclusion string) contextNode {
	return contextNode{
		Typename: "CheckRun",
		CheckRunContext: struct {
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
		}{
			Name:       name,
			Status:     status,
			Conclusion: conclusion,
		},
	}
}

func makeTestQuery(checkRunNames []string, hasNextPage bool, endCursor string, rateLimitRemaining int) *pullRequestQuery {
	q := &pullRequestQuery{
		RateLimit: struct{ Remaining int }{Remaining: rateLimitRemaining},
	}

	var nodes []contextNode
	for _, name := range checkRunNames {
		nodes = append(nodes, makeContextNodeCheckRun(name, "COMPLETED", "SUCCESS"))
	}

	q.Repository.PullRequest.Commits.Nodes = []struct {
		Commit struct {
			StatusCheckRollup struct {
				Contexts struct {
					Nodes    []contextNode
					PageInfo struct {
						HasNextPage bool
						EndCursor   githubv4.String
					}
				} `graphql:"contexts(first: 100, after: $contextsCursor)"`
			}
		}
	}{{}}

	commit := &q.Repository.PullRequest.Commits.Nodes[0]
	commit.Commit.StatusCheckRollup.Contexts.Nodes = nodes
	commit.Commit.StatusCheckRollup.Contexts.PageInfo.HasNextPage = hasNextPage
	commit.Commit.StatusCheckRollup.Contexts.PageInfo.EndCursor = githubv4.String(endCursor)

	return q
}

func TestFetchCheckRunsGraphQL_SinglePage(t *testing.T) {
	mock := &mockQuerier{
		responses: []mockResponse{
			{query: makeTestQuery([]string{"lint", "test"}, false, "", 4999)},
		},
	}

	checkRuns, rateLimit, err := fetchCheckRunsGraphQL(context.Background(), mock, "owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checkRuns) != 2 {
		t.Errorf("expected 2 check runs, got %d", len(checkRuns))
	}
	if rateLimit != 4999 {
		t.Errorf("expected rate limit 4999, got %d", rateLimit)
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 query call, got %d", mock.callCount)
	}
}

func TestFetchCheckRunsGraphQL_MultiPagePagination(t *testing.T) {
	mock := &mockQuerier{
		responses: []mockResponse{
			{query: makeTestQuery([]string{"build"}, true, "cursor-page-1", 4998)},
			{query: makeTestQuery([]string{"lint", "test"}, false, "", 4997)},
		},
	}

	checkRuns, rateLimit, err := fetchCheckRunsGraphQL(context.Background(), mock, "owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checkRuns) != 3 {
		t.Errorf("expected 3 check runs across 2 pages, got %d", len(checkRuns))
	}
	if checkRuns[0].Name != "build" {
		t.Errorf("expected first check 'build', got %q", checkRuns[0].Name)
	}
	if checkRuns[1].Name != "lint" {
		t.Errorf("expected second check 'lint', got %q", checkRuns[1].Name)
	}
	if checkRuns[2].Name != "test" {
		t.Errorf("expected third check 'test', got %q", checkRuns[2].Name)
	}
	if rateLimit != 4997 {
		t.Errorf("expected rate limit 4997, got %d", rateLimit)
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 query calls for pagination, got %d", mock.callCount)
	}
}

func TestFetchCheckRunsGraphQL_EmptyCommits(t *testing.T) {
	q := &pullRequestQuery{
		RateLimit: struct{ Remaining int }{Remaining: 4999},
	}
	mock := &mockQuerier{
		responses: []mockResponse{{query: q}},
	}

	checkRuns, rateLimit, err := fetchCheckRunsGraphQL(context.Background(), mock, "owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checkRuns) != 0 {
		t.Errorf("expected 0 check runs, got %d", len(checkRuns))
	}
	if rateLimit != 4999 {
		t.Errorf("expected rate limit 4999, got %d", rateLimit)
	}
}

func TestFetchCheckRunsGraphQL_QueryError(t *testing.T) {
	mock := &mockQuerier{
		responses: []mockResponse{
			{err: fmt.Errorf("network error")},
		},
	}

	checkRuns, rateLimit, err := fetchCheckRunsGraphQL(context.Background(), mock, "owner", "repo", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if checkRuns != nil {
		t.Errorf("expected nil check runs on error, got %v", checkRuns)
	}
	if rateLimit != 5000 {
		t.Errorf("expected default rate limit 5000 on error, got %d", rateLimit)
	}
}

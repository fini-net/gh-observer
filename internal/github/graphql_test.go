package github

import (
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

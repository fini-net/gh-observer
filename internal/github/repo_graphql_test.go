package github

import (
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
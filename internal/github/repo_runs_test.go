package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v88/github"
)

func TestFetchRepoRunPageSinglePage(t *testing.T) {
	// Serve 3 pages of runs (100 + 50 + 1) and verify fetchRepoRunPage only
	// makes ONE request and returns only the first page's runs — proving it
	// does not follow NextPage. This guards against the 504-causing pagination
	// over hundreds of recent runs on high-traffic repos.
	var requestCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		var runs []map[string]any
		count := 100
		if n > 1 {
			// If the code paginates, return a different (smaller) page so we
			// can detect that the caller followed NextPage.
			count = 50
		}
		for i := 0; i < count; i++ {
			id := int64(n)*1000 + int64(i)
			runs = append(runs, map[string]any{
				"id":           id,
				"name":         "run",
				"head_branch":  "main",
				"head_sha":     "abc",
				"event":        "push",
				"status":       "completed",
				"conclusion":   "success",
				"workflow_id":  42,
				"created_at":   "2026-06-18T00:00:00Z",
				"updated_at":   "2026-06-18T00:01:00Z",
				"run_started_at": "2026-06-18T00:00:00Z",
			})
		}

		body := map[string]any{
			"total_count":   151,
			"workflow_runs": runs,
		}
		// Always claim there's a next page (page 2 exists). If the code
		// follows NextPage it will call back and requestCount will hit 2.
		w.Header().Set("Link", `<https://api.github.com/repos/owner/repo/actions/runs?page=2>; rel="next", <https://api.github.com/repos/owner/repo/actions/runs?page=2>; rel="last"`)
		_ = json.NewEncoder(w).Encode(body)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))

	opts := &github.ListWorkflowRunsOptions{
		ExcludePullRequests: true,
		ListOptions:         github.ListOptions{PerPage: 100},
	}

	runs, _, err := fetchRepoRunPage(context.Background(), client, "owner", "repo", opts)
	if err != nil {
		t.Fatalf("fetchRepoRunPage error: %v", err)
	}

	if got := requestCount.Load(); got != 1 {
		t.Errorf("request count = %d, want 1 (must not paginate)", got)
	}
	if len(runs) != 100 {
		t.Errorf("runs returned = %d, want 100 (first page only)", len(runs))
	}

	// Issue #331: convertBranchRun must copy head_sha from the REST payload
	// so repo-mode dedup can key standalone runs by commit SHA.
	if len(runs) > 0 && runs[0].HeadSHA != "abc" {
		t.Errorf("runs[0].HeadSHA = %q, want %q (convertBranchRun must copy head_sha)", runs[0].HeadSHA, "abc")
	}
}

func TestFetchRepoWorkflowRunsCreatedFilterFormat(t *testing.T) {
	// Verify the Created: date-range filter uses an RFC3339 timestamp (with
	// time component), not time.DateOnly (date only). The date-only form made
	// a 30-minute fade window query the whole calendar day server-side, which
	// returned ~10x more runs and triggered 504s on busy repos.
	var capturedCreated string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		capturedCreated = r.URL.Query().Get("created")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":   0,
			"workflow_runs": []any{},
		})
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))

	_, _, err := FetchRepoWorkflowRuns(context.Background(), client, "owner", "repo", 30*time.Minute)
	if err != nil {
		t.Fatalf("FetchRepoWorkflowRuns error: %v", err)
	}

	if capturedCreated == "" {
		t.Fatal("created query param not sent")
	}
	// RFC3339 contains a 'T' separator and 'Z' (or offset). DateOnly would be
	// bare "YYYY-MM-DD". Assert we see the time component.
	if len(capturedCreated) < len(">=2026-06-18T00:00:00Z") {
		t.Errorf("created filter = %q, want RFC3339 timestamp (with time), got date-only?", capturedCreated)
	}
	if capturedCreated[:2] != ">=" {
		t.Errorf("created filter = %q, want \">=\" prefix", capturedCreated)
	}
	// Strip the ">=" prefix and verify it parses as RFC3339.
	tsStr := capturedCreated[2:]
	if _, err := time.Parse(time.RFC3339, tsStr); err != nil {
		t.Errorf("created filter timestamp %q is not valid RFC3339: %v", tsStr, err)
	}
}
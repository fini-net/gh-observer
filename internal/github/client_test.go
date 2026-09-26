package github

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v92/github"
)

func TestGetTokenFromEnv(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "env-token-123")

	token, err := GetToken()
	if err != nil {
		t.Fatalf("GetToken() error: %v", err)
	}
	if token != "env-token-123" {
		t.Errorf("GetToken() = %q, want %q", token, "env-token-123")
	}
}

func TestGetTokenMissingFails(t *testing.T) {
	// Clear the env var and sabotage the gh CLI fallback so both paths fail.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir()) // empty dir: `gh` not found

	_, err := GetToken()
	if err == nil {
		t.Fatal("GetToken() should fail without token or gh CLI")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %v, want authentication-failed message", err)
	}
}

func TestNewClientFromToken(t *testing.T) {
	client, err := NewClientFromToken("test-token", "")
	if err != nil {
		t.Fatalf("NewClientFromToken() error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClientFromToken() returned nil client")
	}
}

func TestNewClientWithoutTokenFails(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir())

	client, err := NewClient(context.Background(), "")
	if err == nil {
		t.Fatal("NewClient() should fail without credentials")
	}
	if client != nil {
		t.Error("NewClient() should return nil client on error")
	}
}

func TestSafeGraphQLInt(t *testing.T) {
	t.Run("in range converts", func(t *testing.T) {
		v, err := safeGraphQLInt(42)
		if err != nil {
			t.Fatalf("safeGraphQLInt(42) error: %v", err)
		}
		if int(v) != 42 {
			t.Errorf("safeGraphQLInt(42) = %d, want 42", v)
		}
	})

	t.Run("zero converts", func(t *testing.T) {
		if _, err := safeGraphQLInt(0); err != nil {
			t.Errorf("safeGraphQLInt(0) error: %v", err)
		}
	})

	t.Run("above int32 range errors", func(t *testing.T) {
		_, err := safeGraphQLInt(math.MaxInt32 + 1)
		if err == nil {
			t.Error("safeGraphQLInt should reject values above int32 range")
		}
	})

	t.Run("below int32 range errors", func(t *testing.T) {
		if ^uint(0) == math.MaxUint64 { // 64-bit platform
			_, err := safeGraphQLInt(math.MinInt32 - 1)
			if err == nil {
				t.Error("safeGraphQLInt should reject values below int32 range")
			}
		}
	})
}

func TestFailureConclusion(t *testing.T) {
	tests := []struct {
		conclusion string
		want       bool
	}{
		{"failure", true},
		{"timed_out", true},
		{"action_required", true},
		{"success", false},
		{"cancelled", false},
		{"skipped", false},
		{"neutral", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.conclusion, func(t *testing.T) {
			if got := FailureConclusion(tt.conclusion); got != tt.want {
				t.Errorf("FailureConclusion(%q) = %v, want %v", tt.conclusion, got, tt.want)
			}
		})
	}
}

func TestWorkflowJobInfoToCheckRuns(t *testing.T) {
	now := time.Now()
	started := now.Add(-2 * time.Minute)
	completed := now.Add(-1 * time.Minute)

	jobs := []WorkflowJobInfo{
		{
			Name:         "build",
			WorkflowName: "CI",
			Status:       "completed",
			Conclusion:   "success",
			StartedAt:    &github.Timestamp{Time: started},
			CompletedAt:  &github.Timestamp{Time: completed},
			HTMLURL:      "https://github.com/owner/repo/actions/runs/1/job/1",
			RunID:        1,
			WorkflowID:   42,
		},
		{
			Name:   "queued-job",
			Status: "queued",
		},
	}

	runs := WorkflowJobInfoToCheckRuns(jobs)
	if len(runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(runs))
	}

	first := runs[0]
	if first.Name != "build" || first.WorkflowName != "CI" {
		t.Errorf("first = %+v, want name build / workflow CI", first)
	}
	if first.Status != "completed" || first.Conclusion != "success" {
		t.Errorf("first status/conclusion = %q/%q, want completed/success", first.Status, first.Conclusion)
	}
	if first.DetailsURL != jobs[0].HTMLURL {
		t.Errorf("DetailsURL = %q, want %q", first.DetailsURL, jobs[0].HTMLURL)
	}
	if first.WorkflowRunID != 1 || first.WorkflowID != 42 {
		t.Errorf("IDs = %d/%d, want 1/42", first.WorkflowRunID, first.WorkflowID)
	}
	if first.StartedAt == nil || !first.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", first.StartedAt, started)
	}
	if first.CompletedAt == nil || !first.CompletedAt.Equal(completed) {
		t.Errorf("CompletedAt = %v, want %v", first.CompletedAt, completed)
	}

	second := runs[1]
	if second.StartedAt != nil || second.CompletedAt != nil {
		t.Error("nil timestamps must stay nil in conversion")
	}

	t.Run("empty input", func(t *testing.T) {
		if got := WorkflowJobInfoToCheckRuns(nil); len(got) != 0 {
			t.Errorf("WorkflowJobInfoToCheckRuns(nil) = %d runs, want 0", len(got))
		}
	})
}

// newTestClient builds a go-github client pointed at a test server.
func newTestClient(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return client
}

func TestFetchPRInfoREST(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42,
			"title":  "Add widgets",
			"state":  "open",
			"head": map[string]any{
				"sha": "abc123def456",
			},
			"created_at": "2026-09-01T12:34:56Z",
		})
	})
	client := newTestClient(t, handler)

	info, err := FetchPRInfo(context.Background(), client, "owner", "repo", 42)
	if err != nil {
		t.Fatalf("FetchPRInfo() error: %v", err)
	}
	if info.Number != 42 {
		t.Errorf("Number = %d, want 42", info.Number)
	}
	if info.Title != "Add widgets" {
		t.Errorf("Title = %q, want %q", info.Title, "Add widgets")
	}
	if info.HeadSHA != "abc123def456" {
		t.Errorf("HeadSHA = %q, want %q", info.HeadSHA, "abc123def456")
	}
	if info.CreatedAt == "" {
		t.Error("CreatedAt should be formatted non-empty")
	}
}

func TestFetchPRInfoRESTError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
	})
	client := newTestClient(t, handler)

	info, err := FetchPRInfo(context.Background(), client, "owner", "repo", 999)
	if err == nil {
		t.Fatal("FetchPRInfo() should fail on 404")
	}
	if info != nil {
		t.Error("FetchPRInfo() should return nil info on error")
	}
}

func TestFetchCheckRunsForRef(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"check_runs": []map[string]any{
				{"id": 1, "name": "build", "status": "completed", "conclusion": "success"},
				{"id": 2, "name": "test", "status": "in_progress", "conclusion": nil},
			},
		})
	})
	client := newTestClient(t, handler)

	result, err := FetchCheckRuns(context.Background(), client, "owner", "repo", "abc123")
	if err != nil {
		t.Fatalf("FetchCheckRuns() error: %v", err)
	}
	if len(result.CheckRuns) != 2 {
		t.Errorf("CheckRuns = %d, want 2", len(result.CheckRuns))
	}
	// No X-RateLimit headers: go-github decodes Rate.Remaining as 0.
	if result.RateLimitRemaining != 0 {
		t.Errorf("RateLimitRemaining = %d, want 0 (no rate headers sent)", result.RateLimitRemaining)
	}
}

func TestFetchCheckRunsForRefError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	client := newTestClient(t, handler)

	result, err := FetchCheckRuns(context.Background(), client, "owner", "repo", "abc")
	if err == nil {
		t.Fatal("FetchCheckRuns() should fail on 401")
	}
	if result != nil {
		t.Error("FetchCheckRuns() should return nil on error")
	}
}

func TestFetchRunJobsREST(t *testing.T) {
	var requestCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First page: 2 jobs + Link header pointing to page 2.
			w.Header().Set("Link", `<http://example.com?page=2>; rel="next"`)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 3,
				"jobs": []map[string]any{
					{
						"id":            1,
						"name":          "build",
						"workflow_name": "CI",
						"status":        "completed",
						"conclusion":    "success",
						"html_url":      "https://github.com/owner/repo/actions/runs/1/job/1",
						"run_id":        1,
						"started_at":    "2026-09-01T12:00:00Z",
						"completed_at":  "2026-09-01T12:02:00Z",
					},
					{
						"id":     2,
						"name":   "test",
						"status": "queued",
					},
				},
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 3,
				"jobs": []map[string]any{
					{
						"id":            3,
						"name":          "lint",
						"workflow_name": "CI",
						"status":        "completed",
						"conclusion":    "failure",
					},
				},
			})
		}
	})
	client := newTestClient(t, handler)

	jobs, rateLimit, err := FetchRunJobs(context.Background(), client, "owner", "repo", 1)
	if err != nil {
		t.Fatalf("FetchRunJobs() error: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("jobs = %d, want 3 (paginated)", len(jobs))
	}
	if jobs[0].Name != "build" || jobs[0].WorkflowName != "CI" {
		t.Errorf("jobs[0] = %+v, want build/CI", jobs[0])
	}
	if jobs[2].Name != "lint" {
		t.Errorf("jobs[2].Name = %q, want %q", jobs[2].Name, "lint")
	}
	// No X-RateLimit headers: go-github decodes Rate.Remaining as 0.
	if rateLimit != 0 {
		t.Errorf("rateLimit = %d, want 0 (no rate headers sent)", rateLimit)
	}
}

func TestFetchRunJobsRESTError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	client := newTestClient(t, handler)

	jobs, _, err := FetchRunJobs(context.Background(), client, "owner", "repo", 1)
	if err == nil {
		t.Fatal("FetchRunJobs() should fail on 500")
	}
	if jobs != nil {
		t.Error("FetchRunJobs() should return nil jobs on error")
	}
}

func TestFetchRunInfoREST(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             12345,
			"name":           "fallback-name",
			"display_title":  "Real display title",
			"status":         "completed",
			"conclusion":     "success",
			"workflow_id":    42,
			"head_sha":       "abc123",
			"created_at":     "2026-09-01T12:00:00Z",
			"run_started_at": "2026-09-01T12:00:05Z",
			"head_commit": map[string]any{
				"message":   "first line\n\nbody",
				"timestamp": "2026-09-01T11:59:00Z",
			},
		})
	})
	client := newTestClient(t, handler)

	// Empty token: GraphQL lookup skipped, REST fallback used.
	info, rateLimit, err := FetchRunInfo(context.Background(), client, "", "", "owner", "repo", 12345)
	if err != nil {
		t.Fatalf("FetchRunInfo() error: %v", err)
	}
	if info.DisplayTitle != "Real display title" {
		t.Errorf("DisplayTitle = %q, want display_title to win over name", info.DisplayTitle)
	}
	if info.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want %q", info.HeadSHA, "abc123")
	}
	if info.HeadCommitMsg != "first line" {
		t.Errorf("HeadCommitMsg = %q, want %q (first line only)", info.HeadCommitMsg, "first line")
	}
	if info.HeadPushedTime == nil || info.HeadPushedTime.IsZero() {
		t.Error("HeadPushedTime should be set from REST fallback")
	}
	if rateLimit != 5000 {
		t.Errorf("rateLimit = %d, want 5000 (GraphQL skipped)", rateLimit)
	}
	if info.Status != "completed" || info.Conclusion != "success" {
		t.Errorf("status/conclusion = %q/%q", info.Status, info.Conclusion)
	}
}

func TestFetchRunInfoRESTNameFallback(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     1,
			"name":   "workflow-name-title",
			"status": "in_progress",
		})
	})
	client := newTestClient(t, handler)

	info, _, err := FetchRunInfo(context.Background(), client, "", "", "owner", "repo", 1)
	if err != nil {
		t.Fatalf("FetchRunInfo() error: %v", err)
	}
	if info.DisplayTitle != "workflow-name-title" {
		t.Errorf("DisplayTitle = %q, want name fallback", info.DisplayTitle)
	}
}

func TestFetchRunInfoRESTError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	client := newTestClient(t, handler)

	info, _, err := FetchRunInfo(context.Background(), client, "", "", "owner", "repo", 1)
	if err == nil {
		t.Fatal("FetchRunInfo() should fail on 404")
	}
	if info != nil {
		t.Error("FetchRunInfo() should return nil on error")
	}
}

func TestFetchJobAveragesViaREST(t *testing.T) {
	// Two checks: one with a parseable DetailsURL (run 100), one with
	// WorkflowID known directly (77).
	checks := []CheckRunInfo{
		{Name: "build", DetailsURL: "https://github.com/owner/repo/actions/runs/100/job/1"},
		{Name: "scan", WorkflowID: 77},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/100":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          100,
				"workflow_id": 55,
			})
		case "/repos/owner/repo/actions/workflows/55/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{
					{"id": 201},
				},
			})
		case "/repos/owner/repo/actions/workflows/77/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"workflow_runs": []map[string]any{
					{"id": 301},
				},
			})
		case "/repos/owner/repo/actions/runs/201/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"jobs": []map[string]any{
					{
						"name":         "build",
						"status":       "completed",
						"conclusion":   "success",
						"started_at":   "2026-09-01T12:00:00Z",
						"completed_at": "2026-09-01T12:02:00Z",
					},
				},
			})
		case "/repos/owner/repo/actions/runs/301/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"jobs": []map[string]any{
					{
						"name":         "build",
						"status":       "completed",
						"conclusion":   "success",
						"started_at":   "2026-09-01T13:00:00Z",
						"completed_at": "2026-09-01T13:04:00Z",
					},
				},
			})
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	client := newTestClient(t, handler)

	averages, newRunToWF, fetchedWFs, err := FetchJobAverages(context.Background(), client, "owner", "repo", checks, nil, nil)
	if err != nil {
		t.Fatalf("FetchJobAverages() error: %v", err)
	}
	if averages == nil || averages["build"] == 0 {
		t.Fatalf("averages[build] = %v, want positive", averages["build"])
	}
	if newRunToWF[100] != 55 {
		t.Errorf("newRunToWF[100] = %d, want 55", newRunToWF[100])
	}
	if len(fetchedWFs) != 2 {
		t.Errorf("fetchedWFs = %v, want both 55 and 77", fetchedWFs)
	}
}

func TestFetchJobAveragesEmptyChecks(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no requests should be made for empty checks")
	}))

	averages, newRunToWF, fetchedWFs, err := FetchJobAverages(context.Background(), client, "owner", "repo", nil, nil, nil)
	if err != nil {
		t.Fatalf("FetchJobAverages() error: %v", err)
	}
	if averages != nil || newRunToWF != nil || fetchedWFs != nil {
		t.Errorf("all returns should be nil for empty checks, got %v %v %v", averages, newRunToWF, fetchedWFs)
	}
}

func TestFetchJobAveragesAllFetched(t *testing.T) {
	checks := []CheckRunInfo{{Name: "build", WorkflowID: 55}}
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no requests should be made when workflow already fetched")
	}))

	averages, _, fetchedWFs, err := FetchJobAverages(
		context.Background(), client, "owner", "repo", checks,
		nil, map[int64]bool{55: true},
	)
	if err != nil {
		t.Fatalf("FetchJobAverages() error: %v", err)
	}
	if averages != nil {
		t.Error("averages should be nil when nothing new to fetch")
	}
	if fetchedWFs != nil {
		t.Errorf("fetchedWFs = %v, want nil", fetchedWFs)
	}
}

func TestFetchJobAveragesCachedRunIDLookup(t *testing.T) {
	// Run 100 already mapped to workflow 55 in the cache: no
	// GetWorkflowRunByID call should be made, only runs+jobs lookups.
	checks := []CheckRunInfo{
		{Name: "build", DetailsURL: "https://github.com/owner/repo/actions/runs/100/job/1"},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/actions/runs/100" {
			t.Errorf("GetWorkflowRunByID should not be called for cached run ID: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// ListWorkflowRunsByID (workflow 55) and any jobs lookups.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":   0,
			"workflow_runs": []map[string]any{},
		})
	})
	client := newTestClient(t, handler)

	_, newRunToWF, fetchedWFs, err := FetchJobAverages(
		context.Background(), client, "owner", "repo", checks,
		map[int64]int64{100: 55}, nil,
	)
	if err != nil {
		t.Fatalf("FetchJobAverages() error: %v", err)
	}
	if len(newRunToWF) != 0 {
		t.Errorf("newRunToWF = %v, want empty (cache hit)", newRunToWF)
	}
	if len(fetchedWFs) != 1 || fetchedWFs[0] != 55 {
		t.Errorf("fetchedWFs = %v, want [55]", fetchedWFs)
	}
}

func TestEnrichRepoRunsWithJobs(t *testing.T) {
	runs := []BranchRunData{
		{RunID: 1, DisplayTitle: "Run 1"},
		{RunID: 2, DisplayTitle: "Run 2"},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/1/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"jobs": []map[string]any{
					{
						"name":          "build",
						"workflow_name": "CI",
						"status":        "completed",
						"conclusion":    "success",
						"started_at":    "2026-09-01T12:00:00Z",
						"completed_at":  "2026-09-01T12:01:00Z",
					},
				},
			})
		case "/repos/owner/repo/actions/runs/2/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 0,
				"jobs":        []map[string]any{},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	client := newTestClient(t, handler)

	enriched, rateLimit, err := EnrichRepoRunsWithJobs(context.Background(), client, "owner", "repo", runs)
	if err != nil {
		t.Fatalf("EnrichRepoRunsWithJobs() error: %v", err)
	}
	if len(enriched) != 2 {
		t.Fatalf("enriched = %d runs, want 2", len(enriched))
	}
	if len(enriched[0].Jobs) != 1 {
		t.Errorf("run 1 jobs = %d, want 1", len(enriched[0].Jobs))
	}
	if enriched[0].WorkflowName != "CI" {
		t.Errorf("run 1 WorkflowName = %q, want CI (copied from first job)", enriched[0].WorkflowName)
	}
	if len(enriched[1].Jobs) != 0 {
		t.Errorf("run 2 jobs = %d, want 0", len(enriched[1].Jobs))
	}
	// No X-RateLimit headers: FetchRunJobs reports 0, which EnrichRepoRunsWithJobs
	// takes as the minimum observed.
	if rateLimit != 0 {
		t.Errorf("rateLimit = %d, want 0 (no rate headers sent)", rateLimit)
	}
}

func TestEnrichRepoRunsJobsFailureNonFatal(t *testing.T) {
	runs := []BranchRunData{{RunID: 1, DisplayTitle: "Run 1", Status: "in_progress"}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	client := newTestClient(t, handler)

	enriched, _, err := EnrichRepoRunsWithJobs(context.Background(), client, "owner", "repo", runs)
	if err != nil {
		t.Fatalf("job-fetch failure must be non-fatal, got error: %v", err)
	}
	if len(enriched) != 1 {
		t.Fatalf("enriched = %d, want run kept with empty jobs", len(enriched))
	}
	if len(enriched[0].Jobs) != 0 {
		t.Errorf("jobs = %d, want 0 on failed fetch", len(enriched[0].Jobs))
	}
}

func TestIsJujutsu(t *testing.T) {
	// Run from this repo's root — a plain git repo without .jj.
	resetJJDetection()
	if IsJujutsu() {
		t.Error("IsJujutsu() in a plain git repo should be false")
	}
	resetJJDetection()
}

func TestIsJujutsuWithJJRepo(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatalf("mkdir .jj: %v", err)
	}
	t.Chdir(tmpDir)
	t.Cleanup(resetJJDetection)
	resetJJDetection()

	// `jj git root` fails (jj binary may not exist), so detection reports
	// false — but the important part is it doesn't panic and caches the
	// result.
	got := IsJujutsu()
	if got {
		t.Log("jj binary present and colocation resolved; detected true")
	} else {
		t.Log("jj binary absent or failed; detected false")
	}
}

func TestFindJJGitRootFailsWithoutJJ(t *testing.T) {
	t.Chdir(t.TempDir())

	_, found, err := findJJGitRoot()
	if err != nil {
		t.Fatalf("findJJGitRoot() error in plain dir: %v", err)
	}
	if found {
		t.Error("findJJGitRoot() should not find .jj in an empty dir")
	}
}

func TestFindJJGitRootEmptyGitRoot(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatalf("mkdir .jj: %v", err)
	}
	t.Chdir(tmpDir)

	// jj on PATH? If not, the command fails and errors. Either way the
	// function must return an error or found=false with an empty root.
	root, found, err := findJJGitRoot()
	if err == nil && found && root == "" {
		t.Error("findJJGitRoot() must not return found=true with empty root")
	}
}

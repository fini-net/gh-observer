package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-github/v89/github"
)

func ptrTo(s string) *string {
	return &s
}

// weightedBuild2Run is the exponentially decayed weighted average for the
// "fetches and averages job durations across runs" case: build durations are
// 2min (newest, run 1) and 4min (oldest, run 2). With decay=0.7 that is
// (2min*1 + 4min*0.7)/(1+0.7) = 169411764705ns (~2m49.4s), well below the flat
// mean of 3min.
const weightedBuild2Run = 169411764705 * time.Nanosecond

// weightedBuild3RunNewShort is the weighted average for the
// "newer shorter run pulls average below flat mean" case: build durations are
// 2min (newest), 6min, 6min. With decay=0.7 that is
// (2min*1 + 6min*0.7 + 6min*0.49)/(1+0.7+0.49) = 250410958904ns (~4m10.4s),
// below the flat mean of 4m40s but above the newest 2min.
const weightedBuild3RunNewShort = 250410958904 * time.Nanosecond

func TestParseRunIDFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  int64
		wantErr bool
	}{
		{
			name:   "valid CheckRun URL",
			url:    "https://github.com/owner/repo/actions/runs/12345678/job/987654321",
			wantID: 12345678,
		},
		{
			name:    "StatusContext URL without run ID",
			url:     "https://github.com/owner/repo/commit/abc123/checks",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "AdvSec /runs/ URL is not matched",
			url:     "https://github.com/owner/repo/runs/73263098935",
			wantErr: true,
		},
		{
			name:    "external app URL is not matched",
			url:     "https://probot.github.io/apps/dco/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRunIDFromURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRunIDFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantID {
				t.Errorf("ParseRunIDFromURL() = %v, want %v", got, tt.wantID)
			}
		})
	}
}

func TestWeightedAverage(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		want      time.Duration
	}{
		{name: "empty returns zero", durations: nil, want: 0},
		{name: "single returns itself", durations: []time.Duration{5 * time.Minute}, want: 5 * time.Minute},
		{name: "two equal collapse to that value", durations: []time.Duration{4 * time.Minute, 4 * time.Minute}, want: 4 * time.Minute},
		{
			name:      "newest dominates two-run average",
			durations: []time.Duration{2 * time.Minute, 4 * time.Minute},
			want:      weightedBuild2Run,
		},
		{
			name:      "newer shorter run below flat mean above newest",
			durations: []time.Duration{2 * time.Minute, 6 * time.Minute, 6 * time.Minute},
			want:      weightedBuild3RunNewShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weightedAverage(tt.durations)
			if got != tt.want {
				t.Errorf("weightedAverage(%v) = %v, want %v", tt.durations, got, tt.want)
			}
		})
	}

	// Sanity bounds for the new-shorter-run case: the weighted average must
	// sit strictly below the flat mean (4m40s) and strictly above the newest
	// run (2min), demonstrating that recency weighting pulled the estimate
	// toward the recent (shorter) runs.
	flatMean := (2*time.Minute + 6*time.Minute + 6*time.Minute) / 3
	got := weightedAverage([]time.Duration{2 * time.Minute, 6 * time.Minute, 6 * time.Minute})
	if got >= flatMean {
		t.Errorf("weightedAverage should be below flat mean %v, got %v", flatMean, got)
	}
	if got <= 2*time.Minute {
		t.Errorf("weightedAverage should stay above newest run %v, got %v", 2*time.Minute, got)
	}
}

func TestAverageJobDurations(t *testing.T) {
	tests := []struct {
		name         string
		mockHandler  http.HandlerFunc
		runIDs       []int64
		wantAverages map[string]time.Duration
	}{
		{
			name:   "empty run IDs returns nil",
			runIDs: []int64{},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantAverages: nil,
		},
		{
			name:   "fetches and averages job durations across runs",
			runIDs: []int64{1, 2},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs" {
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:02:00Z"},
						{"name":"test","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:03:00Z"}
					]}`))
				} else if r.URL.Path == "/repos/owner/repo/actions/runs/2/jobs" {
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:04:00Z"}
					]}`))
				}
			},
			wantAverages: map[string]time.Duration{
				"build": weightedBuild2Run,
				"test":  3 * time.Minute,
			},
		},
		{
			name:   "skips jobs missing timestamps",
			runIDs: []int64{1},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs" {
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:01:00Z"},
						{"name":"broken"}
					]}`))
				}
			},
			wantAverages: map[string]time.Duration{
				"build": time.Minute,
			},
		},
		{
			name:   "newer shorter run pulls weighted average below flat mean",
			runIDs: []int64{1, 2, 3},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/repos/owner/repo/actions/runs/1/jobs":
					// newest run: 2min
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:02:00Z"}
					]}`))
				case "/repos/owner/repo/actions/runs/2/jobs":
					// older run: 6min
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:06:00Z"}
					]}`))
				case "/repos/owner/repo/actions/runs/3/jobs":
					// oldest run: 6min
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:06:00Z"}
					]}`))
				}
			},
			wantAverages: map[string]time.Duration{
				"build": weightedBuild3RunNewShort,
			},
		},
		{
			name:   "API error on one run skips it",
			runIDs: []int64{1, 2},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs" {
					w.WriteHeader(http.StatusInternalServerError)
				} else if r.URL.Path == "/repos/owner/repo/actions/runs/2/jobs" {
					w.Write([]byte(`{"jobs":[
						{"name":"lint","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:00:30Z"}
					]}`))
				}
			},
			wantAverages: map[string]time.Duration{
				"lint": 30 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.mockHandler)
			defer server.Close()
			client, _ := github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))

			averages := averageJobDurations(
				context.Background(),
				client,
				"owner",
				"repo",
				tt.runIDs,
			)

			if tt.wantAverages == nil {
				if averages != nil {
					t.Errorf("averageJobDurations() averages = %v, want nil", averages)
				}
			} else {
				for k, v := range tt.wantAverages {
					if averages[k] != v {
						t.Errorf("averageJobDurations() averages[%s] = %v, want %v", k, averages[k], v)
					}
				}
			}
		})
	}
}

func TestDiscoverWorkflows(t *testing.T) {
	tests := []struct {
		name                    string
		checkRuns               []CheckRunInfo
		knownRunIDToWorkflowID  map[int64]int64
		knownFetchedWorkflowIDs map[int64]bool
		mockHandler             http.HandlerFunc
		wantRunIDs              map[int64]int64
		wantWorkflowIDs         []int64
		wantErr                 bool
	}{
		{
			name:                    "empty check runs returns nil",
			checkRuns:               []CheckRunInfo{},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler:             nil,
			wantRunIDs:              nil,
			wantWorkflowIDs:         nil,
		},
		{
			name: "check run without DetailsURL is skipped",
			checkRuns: []CheckRunInfo{
				{Name: "test", Status: "completed"},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler:             nil,
			wantRunIDs:              nil,
			wantWorkflowIDs:         nil,
		},
		{
			name: "cached run ID mapping is reused",
			checkRuns: []CheckRunInfo{
				{Name: "test", Status: "completed", DetailsURL: "https://github.com/owner/repo/actions/runs/123/job/456"},
			},
			knownRunIDToWorkflowID:  map[int64]int64{123: 789},
			knownFetchedWorkflowIDs: map[int64]bool{789: true},
			mockHandler:             nil,
			wantRunIDs:              map[int64]int64{},
			wantWorkflowIDs:         nil,
		},
		{
			name: "fetches workflow run for new run ID",
			checkRuns: []CheckRunInfo{
				{Name: "test", Status: "completed", DetailsURL: "https://github.com/owner/repo/actions/runs/123/job/456"},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/actions/runs/123" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"id":123,"workflow_id":789}`))
				}
			},
			wantRunIDs:      map[int64]int64{123: 789},
			wantWorkflowIDs: []int64{789},
		},
		{
			name: "skips unfetched workflow IDs",
			checkRuns: []CheckRunInfo{
				{Name: "test", Status: "completed", DetailsURL: "https://github.com/owner/repo/actions/runs/123/job/456"},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{789: true},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/actions/runs/123" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"id":123,"workflow_id":789}`))
				}
			},
			wantRunIDs:      map[int64]int64{123: 789},
			wantWorkflowIDs: nil,
		},
		{
			name: "GraphQL WorkflowID skips API call",
			checkRuns: []CheckRunInfo{
				{Name: "test", Status: "completed", WorkflowRunID: 123, WorkflowID: 789},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler:             nil,
			wantRunIDs:              map[int64]int64{123: 789},
			wantWorkflowIDs:         []int64{789},
		},
		{
			name: "GraphQL WorkflowRunID resolves via API",
			checkRuns: []CheckRunInfo{
				{Name: "test", Status: "completed", WorkflowRunID: 123},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/actions/runs/123" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"id":123,"workflow_id":789}`))
				}
			},
			wantRunIDs:      map[int64]int64{123: 789},
			wantWorkflowIDs: []int64{789},
		},
		{
			name: "AdvSec /runs/ URL skipped gracefully",
			checkRuns: []CheckRunInfo{
				{Name: "CodeQL", Status: "completed", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/73263098935"},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler:             nil,
			wantRunIDs:              nil,
			wantWorkflowIDs:         nil,
		},
		{
			name: "DCO external URL skipped gracefully",
			checkRuns: []CheckRunInfo{
				{Name: "DCO", Status: "completed", AppName: "DCO", DetailsURL: "https://probot.github.io/apps/dco/"},
			},
			knownRunIDToWorkflowID:  map[int64]int64{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			mockHandler:             nil,
			wantRunIDs:              nil,
			wantWorkflowIDs:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *github.Client
			if tt.mockHandler != nil {
				server := httptest.NewServer(tt.mockHandler)
				defer server.Close()
				client, _ = github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))
			} else {
				client, _ = github.NewClient()
			}

			runIDs, workflowIDs, err := DiscoverWorkflows(
				context.Background(),
				client,
				"owner",
				"repo",
				tt.checkRuns,
				tt.knownRunIDToWorkflowID,
				tt.knownFetchedWorkflowIDs,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverWorkflows() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantRunIDs == nil {
				if len(runIDs) > 0 {
					t.Errorf("DiscoverWorkflows() runIDs = %v, want empty", runIDs)
				}
			} else {
				if len(tt.wantRunIDs) == 0 && len(runIDs) > 0 {
					t.Errorf("DiscoverWorkflows() runIDs = %v, want empty", runIDs)
				}
				for k, v := range tt.wantRunIDs {
					if runIDs[k] != v {
						t.Errorf("DiscoverWorkflows() runIDs[%d] = %v, want %v", k, runIDs[k], v)
					}
				}
			}

			if tt.wantWorkflowIDs == nil {
				if workflowIDs != nil {
					t.Errorf("DiscoverWorkflows() workflowIDs = %v, want nil", workflowIDs)
				}
			} else {
				if len(workflowIDs) != len(tt.wantWorkflowIDs) {
					t.Errorf("DiscoverWorkflows() len(workflowIDs) = %d, want %d", len(workflowIDs), len(tt.wantWorkflowIDs))
				}
			}
		})
	}
}

func TestDiscoverAdvSecWorkflows(t *testing.T) {
	tests := []struct {
		name                    string
		checkRuns               []CheckRunInfo
		knownFetchedWorkflowIDs map[int64]bool
		wantMatches             map[string]int64
		wantWorkflowIDs         []int64
	}{
		{
			name:                    "empty check runs returns empty",
			checkRuns:               []CheckRunInfo{},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{},
			wantWorkflowIDs:         nil,
		},
		{
			name: "AdvSec CodeQL matched to github-actions CodeQL workflow",
			checkRuns: []CheckRunInfo{
				{Name: "Analyze (go)", WorkflowName: "CodeQL", WorkflowID: 789, AppName: "GitHub Actions"},
				{Name: "CodeQL", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/73263098935"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{"CodeQL": 789},
			wantWorkflowIDs:         []int64{789},
		},
		{
			name: "AdvSec Checkov matched to github-actions Checkov workflow",
			checkRuns: []CheckRunInfo{
				{Name: "scan", WorkflowName: "Checkov", WorkflowID: 456, AppName: "GitHub Actions"},
				{Name: "Checkov", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/12345"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{"Checkov": 456},
			wantWorkflowIDs:         []int64{456},
		},
		{
			name: "AdvSec skipped when no matching workflow name",
			checkRuns: []CheckRunInfo{
				{Name: "scan", WorkflowName: "CI", WorkflowID: 111, AppName: "GitHub Actions"},
				{Name: "CodeQL", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/73263098935"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{},
			wantWorkflowIDs:         nil,
		},
		{
			name: "AdvSec skipped when workflow already fetched",
			checkRuns: []CheckRunInfo{
				{Name: "Analyze (go)", WorkflowName: "CodeQL", WorkflowID: 789, AppName: "GitHub Actions"},
				{Name: "CodeQL", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/73263098935"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{789: true},
			wantMatches:             map[string]int64{"CodeQL": 789},
			wantWorkflowIDs:         nil,
		},
		{
			name: "DCO external app skipped",
			checkRuns: []CheckRunInfo{
				{Name: "DCO", AppName: "DCO", DetailsURL: "https://probot.github.io/apps/dco/"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{},
			wantWorkflowIDs:         nil,
		},
		{
			name: "AdvSec check with WorkflowID already set is skipped",
			checkRuns: []CheckRunInfo{
				{Name: "Analyze (go)", WorkflowName: "CodeQL", WorkflowID: 789, AppName: "GitHub Actions"},
				{Name: "CodeQL", AppName: "GitHub Advanced Security", WorkflowID: 789, DetailsURL: "https://github.com/owner/repo/actions/runs/123/job/456"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{},
			wantWorkflowIDs:         nil,
		},
		{
			name: "multiple AdvSec checks matched",
			checkRuns: []CheckRunInfo{
				{Name: "Analyze (go)", WorkflowName: "CodeQL", WorkflowID: 789, AppName: "GitHub Actions"},
				{Name: "scan", WorkflowName: "Checkov", WorkflowID: 456, AppName: "GitHub Actions"},
				{Name: "CodeQL", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/73263098935"},
				{Name: "Checkov", AppName: "GitHub Advanced Security", DetailsURL: "https://github.com/owner/repo/runs/12345"},
			},
			knownFetchedWorkflowIDs: map[int64]bool{},
			wantMatches:             map[string]int64{"CodeQL": 789, "Checkov": 456},
			wantWorkflowIDs:         []int64{789, 456},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, workflowIDs := DiscoverAdvSecWorkflows(tt.checkRuns, tt.knownFetchedWorkflowIDs)

			if len(tt.wantMatches) == 0 {
				if len(matches) > 0 {
					t.Errorf("DiscoverAdvSecWorkflows() matches = %v, want empty", matches)
				}
			} else {
				for k, v := range tt.wantMatches {
					if matches[k] != v {
						t.Errorf("DiscoverAdvSecWorkflows() matches[%q] = %d, want %d", k, matches[k], v)
					}
				}
			}

			if tt.wantWorkflowIDs == nil {
				if workflowIDs != nil {
					t.Errorf("DiscoverAdvSecWorkflows() workflowIDs = %v, want nil", workflowIDs)
				}
			} else {
				if len(workflowIDs) != len(tt.wantWorkflowIDs) {
					t.Errorf("DiscoverAdvSecWorkflows() len(workflowIDs) = %d, want %d", len(workflowIDs), len(tt.wantWorkflowIDs))
				}
			}
		})
	}
}

func TestFetchWorkflowHistory(t *testing.T) {
	tests := []struct {
		name         string
		workflowID   int64
		mockHandler  http.HandlerFunc
		wantAverages map[string]time.Duration
		wantErr      bool
	}{
		{
			name:       "no runs returns nil",
			workflowID: 789,
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/actions/workflows/789/runs" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"workflow_runs":[]}`))
				}
			},
			wantAverages: nil,
		},
		{
			name:       "fetches and averages job durations",
			workflowID: 789,
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/repos/owner/repo/actions/workflows/789/runs" {
					w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
				} else if r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs" {
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:01:00Z"},
						{"name":"test","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:02:00Z"}
					]}`))
				}
			},
			wantAverages: map[string]time.Duration{
				"build": time.Minute,
				"test":  2 * time.Minute,
			},
		},
		{
			name:       "skips jobs missing timestamps",
			workflowID: 789,
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/repos/owner/repo/actions/workflows/789/runs" {
					w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
				} else if r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs" {
					w.Write([]byte(`{"jobs":[
						{"name":"build","started_at":"2024-01-01T00:00:00Z","completed_at":"2024-01-01T00:01:00Z"},
						{"name":"broken"}
					]}`))
				}
			},
			wantAverages: map[string]time.Duration{
				"build": time.Minute,
			},
		},
		{
			name:       "handles API error on runs",
			workflowID: 789,
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/actions/workflows/789/runs" {
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.mockHandler)
			defer server.Close()
			client, _ := github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))

			averages, err := FetchWorkflowHistory(
				context.Background(),
				client,
				"owner",
				"repo",
				tt.workflowID,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("FetchWorkflowHistory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantAverages == nil {
				if averages != nil {
					t.Errorf("FetchWorkflowHistory() averages = %v, want nil", averages)
				}
			} else {
				for k, v := range tt.wantAverages {
					if averages[k] != v {
						t.Errorf("FetchWorkflowHistory() averages[%s] = %v, want %v", k, averages[k], v)
					}
				}
			}
		})
	}
}

func TestIsExternalAppCheck(t *testing.T) {
	tests := []struct {
		name string
		cr   CheckRunInfo
		want bool
	}{
		{
			name: "DCO external app is external",
			cr: CheckRunInfo{
				Name:       "DCO",
				AppName:    "DCO",
				DetailsURL: "https://probot.github.io/apps/dco/",
			},
			want: true,
		},
		{
			name: "GitHub Actions check with WorkflowID is not external",
			cr: CheckRunInfo{
				Name:       "build",
				WorkflowID: 789,
				DetailsURL: "https://github.com/owner/repo/actions/runs/123/job/456",
			},
			want: false,
		},
		{
			name: "GitHub Actions check with WorkflowRunID is not external",
			cr: CheckRunInfo{
				Name:          "build",
				WorkflowRunID: 123,
				DetailsURL:    "https://github.com/owner/repo/actions/runs/123/job/456",
			},
			want: false,
		},
		{
			name: "AdvSec check with parseable Actions run URL is not external",
			cr: CheckRunInfo{
				Name:       "CodeQL",
				AppName:    "GitHub Advanced Security",
				DetailsURL: "https://github.com/owner/repo/actions/runs/12345678/job/987654321",
			},
			want: false,
		},
		{
			name: "AdvSec check with /runs/ URL is not external (GitHub-hosted, handled via aliasing)",
			cr: CheckRunInfo{
				Name:       "CodeQL",
				AppName:    "GitHub Advanced Security",
				DetailsURL: "https://github.com/owner/repo/runs/73263098935",
			},
			want: false,
		},
		{
			name: "Actions check with /actions/runs/<id> URL (no /job/) is not external (GitHub-hosted)",
			cr: CheckRunInfo{
				Name:       "build",
				AppName:    "GitHub Actions",
				DetailsURL: "https://github.com/owner/repo/actions/runs/12345678",
			},
			want: false,
		},
		{
			name: "check with no AppName and no DetailsURL is not external",
			cr: CheckRunInfo{
				Name: "lint",
			},
			want: false,
		},
		{
			name: "check with only AppName but no DetailsURL is not external (cannot classify)",
			cr: CheckRunInfo{
				Name:    "DCO",
				AppName: "DCO",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsExternalAppCheck(tt.cr)
			if got != tt.want {
				t.Errorf("IsExternalAppCheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyPresumedAverages(t *testing.T) {
	dco := CheckRunInfo{
		Name:       "DCO",
		AppName:    "DCO",
		DetailsURL: "https://probot.github.io/apps/dco/",
	}
	build := CheckRunInfo{
		Name:          "build",
		WorkflowID:    789,
		WorkflowRunID: 123,
		DetailsURL:    "https://github.com/owner/repo/actions/runs/123/job/456",
	}
	codeQLAdvSec := CheckRunInfo{
		Name:       "CodeQL",
		AppName:    "GitHub Advanced Security",
		DetailsURL: "https://github.com/owner/repo/runs/73263098935",
	}
	// external-but-not-in-map uses an off-site URL so IsExternalAppCheck
	// classifies it as external (GitHub-hosted /runs/ URLs are non-external
	// after the githubHostedURLRegexp fix).
	externalNotInMap := CheckRunInfo{
		Name:       "Worfload Bot",
		AppName:    "Worfload",
		DetailsURL: "https://example.com/worfload/status",
	}

	t.Run("injects presumed average for DCO", func(t *testing.T) {
		jobAverages := map[string]time.Duration{}
		presumed := map[string]time.Duration{"DCO": 1 * time.Second}
		ApplyPresumedAverages(jobAverages, []CheckRunInfo{dco, build}, presumed)
		if jobAverages["DCO"] != 1*time.Second {
			t.Errorf("jobAverages[DCO] = %v, want 1s", jobAverages["DCO"])
		}
		if _, present := jobAverages["build"]; present {
			t.Errorf("jobAverages[build] should not be set, got %v", jobAverages["build"])
		}
	})

	t.Run("does not overwrite existing real history", func(t *testing.T) {
		jobAverages := map[string]time.Duration{"DCO": 5 * time.Second}
		presumed := map[string]time.Duration{"DCO": 1 * time.Second}
		ApplyPresumedAverages(jobAverages, []CheckRunInfo{dco}, presumed)
		if jobAverages["DCO"] != 5*time.Second {
			t.Errorf("jobAverages[DCO] = %v, want 5s (should not overwrite existing)", jobAverages["DCO"])
		}
	})

	t.Run("skips check names not in presumed map", func(t *testing.T) {
		jobAverages := map[string]time.Duration{}
		presumed := map[string]time.Duration{"DCO": 1 * time.Second}
		// externalNotInMap is external but not in the presumed map
		ApplyPresumedAverages(jobAverages, []CheckRunInfo{externalNotInMap}, presumed)
		if _, present := jobAverages["Worfload Bot"]; present {
			t.Errorf("jobAverages[Worfload Bot] should not be set, got %v", jobAverages["Worfload Bot"])
		}
	})

	t.Run("no-op when presumed map is empty", func(t *testing.T) {
		jobAverages := map[string]time.Duration{}
		ApplyPresumedAverages(jobAverages, []CheckRunInfo{dco}, nil)
		if len(jobAverages) != 0 {
			t.Errorf("jobAverages should be empty, got %v", jobAverages)
		}
	})

	t.Run("no-op when jobAverages is nil", func(t *testing.T) {
		presumed := map[string]time.Duration{"DCO": 1 * time.Second}
		// Should not panic
		ApplyPresumedAverages(nil, []CheckRunInfo{dco}, presumed)
	})

	t.Run("no-op when no external app checks present", func(t *testing.T) {
		jobAverages := map[string]time.Duration{}
		presumed := map[string]time.Duration{"DCO": 1 * time.Second}
		ApplyPresumedAverages(jobAverages, []CheckRunInfo{build}, presumed)
		if len(jobAverages) != 0 {
			t.Errorf("jobAverages should be empty, got %v", jobAverages)
		}
	})

	t.Run("AdvSec /runs/ URL is not external, presumed average does not apply", func(t *testing.T) {
		jobAverages := map[string]time.Duration{}
		// A presumed average for "CodeQL" must not be injected because the
		// AdvSec /runs/ URL is GitHub-hosted and should be handled by AdvSec
		// aliasing instead, not presumed averages.
		presumed := map[string]time.Duration{"CodeQL": 30 * time.Second}
		ApplyPresumedAverages(jobAverages, []CheckRunInfo{codeQLAdvSec}, presumed)
		if _, present := jobAverages["CodeQL"]; present {
			t.Errorf("jobAverages[CodeQL] should not be set for AdvSec /runs/ URL, got %v", jobAverages["CodeQL"])
		}
	})
}

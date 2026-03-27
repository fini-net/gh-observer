package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-github/v84/github"
)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *github.Client
			if tt.mockHandler != nil {
				server := httptest.NewServer(tt.mockHandler)
				defer server.Close()
				client = github.NewClient(nil)
				client.BaseURL, _ = client.BaseURL.Parse(server.URL + "/")
			} else {
				client = github.NewClient(nil)
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
				if runIDs != nil {
					t.Errorf("DiscoverWorkflows() runIDs = %v, want nil", runIDs)
				}
			} else {
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
			client := github.NewClient(nil)
			client.BaseURL, _ = client.BaseURL.Parse(server.URL + "/")

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

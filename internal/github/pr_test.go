package github

import (
	"strings"
	"testing"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPRNum int
		wantErr   bool
	}{
		{
			name:      "valid HTTPS URL",
			url:       "https://github.com/fini-net/gh-observer/pull/88",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantPRNum: 88,
			wantErr:   false,
		},
		{
			name:      "valid HTTP URL",
			url:       "http://github.com/owner/repo/pull/123",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPRNum: 123,
			wantErr:   false,
		},
		{
			name:      "owner with hyphens and numbers",
			url:       "https://github.com/org-123/repo-name/pull/456",
			wantOwner: "org-123",
			wantRepo:  "repo-name",
			wantPRNum: 456,
			wantErr:   false,
		},
		{
			name:      "repo with dots",
			url:       "https://github.com/owner/repo.name/pull/789",
			wantOwner: "owner",
			wantRepo:  "repo.name",
			wantPRNum: 789,
			wantErr:   false,
		},
		{
			name:    "missing protocol",
			url:     "github.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "wrong host",
			url:     "https://gitlab.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "wrong path format - issues",
			url:     "https://github.com/owner/repo/issues/123",
			wantErr: true,
		},
		{
			name:    "missing pull number",
			url:     "https://github.com/owner/repo/pull/",
			wantErr: true,
		},
		{
			name:    "non-numeric PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:    "trailing slash",
			url:     "https://github.com/owner/repo/pull/123/",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "just github.com",
			url:     "https://github.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotRepo, gotPRNum, err := ParsePRURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePRURL(%q) expected error, got nil", tt.url)
				}
				return
			}
			if err != nil {
				t.Errorf("ParsePRURL(%q) unexpected error: %v", tt.url, err)
				return
			}
			if gotOwner != tt.wantOwner {
				t.Errorf("ParsePRURL(%q) owner = %q, want %q", tt.url, gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("ParsePRURL(%q) repo = %q, want %q", tt.url, gotRepo, tt.wantRepo)
			}
			if gotPRNum != tt.wantPRNum {
				t.Errorf("ParsePRURL(%q) PR number = %d, want %d", tt.url, gotPRNum, tt.wantPRNum)
			}
		})
	}
}

func TestParsePRViewWithRepo(t *testing.T) {
	tests := []struct {
		name       string
		jsonInput  string
		wantNumber int
		wantOwner  string
		wantRepo   string
		wantErr    bool
		errContain string
	}{
		{
			name:       "valid PR view output",
			jsonInput:  `{"number":4173,"url":"https://github.com/StackExchange/dnscontrol/pull/4173"}`,
			wantNumber: 4173,
			wantOwner:  "StackExchange",
			wantRepo:   "dnscontrol",
			wantErr:    false,
		},
		{
			name:       "fork scenario - upstream repo in URL",
			jsonInput:  `{"number":123,"url":"https://github.com/upstream-owner/upstream-repo/pull/123"}`,
			wantNumber: 123,
			wantOwner:  "upstream-owner",
			wantRepo:   "upstream-repo",
			wantErr:    false,
		},
		{
			name:       "owner with hyphens and numbers",
			jsonInput:  `{"number":456,"url":"https://github.com/org-123/repo-name-789/pull/456"}`,
			wantNumber: 456,
			wantOwner:  "org-123",
			wantRepo:   "repo-name-789",
			wantErr:    false,
		},
		{
			name:       "repo with dots",
			jsonInput:  `{"number":1,"url":"https://github.com/owner/repo.name/pull/1"}`,
			wantNumber: 1,
			wantOwner:  "owner",
			wantRepo:   "repo.name",
			wantErr:    false,
		},
		{
			name:       "missing number",
			jsonInput:  `{"url":"https://github.com/owner/repo/pull/123"}`,
			wantNumber: 0,
			wantOwner:  "",
			wantRepo:   "",
			wantErr:    true,
			errContain: "PR number is zero or missing",
		},
		{
			name:       "missing URL",
			jsonInput:  `{"number":123}`,
			wantNumber: 0,
			wantOwner:  "",
			wantRepo:   "",
			wantErr:    true,
			errContain: "PR URL is missing",
		},
		{
			name:       "invalid URL format",
			jsonInput:  `{"number":123,"url":"https://github.com/owner/repo/issues/123"}`,
			wantNumber: 0,
			wantOwner:  "",
			wantRepo:   "",
			wantErr:    true,
			errContain: "failed to parse PR URL",
		},
		{
			name:       "invalid JSON",
			jsonInput:  `{invalid json`,
			wantNumber: 0,
			wantOwner:  "",
			wantRepo:   "",
			wantErr:    true,
			errContain: "failed to parse PR info",
		},
		{
			name:       "empty JSON object",
			jsonInput:  `{}`,
			wantNumber: 0,
			wantOwner:  "",
			wantRepo:   "",
			wantErr:    true,
			errContain: "PR number is zero or missing",
		},
		{
			name:       "PR number mismatch in URL",
			jsonInput:  `{"number":123,"url":"https://github.com/owner/repo/pull/456"}`,
			wantNumber: 0,
			wantOwner:  "",
			wantRepo:   "",
			wantErr:    true,
			errContain: "PR number mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNumber, gotOwner, gotRepo, err := parsePRViewWithRepo([]byte(tt.jsonInput))
			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePRViewWithRepo(%q) expected error, got nil", tt.jsonInput)
				} else if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("parsePRViewWithRepo(%q) error = %v, want error containing %q", tt.jsonInput, err, tt.errContain)
				}
				return
			}
			if err != nil {
				t.Errorf("parsePRViewWithRepo(%q) unexpected error: %v", tt.jsonInput, err)
				return
			}
			if gotNumber != tt.wantNumber {
				t.Errorf("parsePRViewWithRepo(%q) number = %d, want %d", tt.jsonInput, gotNumber, tt.wantNumber)
			}
			if gotOwner != tt.wantOwner {
				t.Errorf("parsePRViewWithRepo(%q) owner = %q, want %q", tt.jsonInput, gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("parsePRViewWithRepo(%q) repo = %q, want %q", tt.jsonInput, gotRepo, tt.wantRepo)
			}
		})
	}
}

func TestParseOwnerRepoFromURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "SSH format with .git",
			url:       "git@github.com:fini-net/gh-observer.git",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantErr:   false,
		},
		{
			name:      "SSH format without .git",
			url:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "HTTPS format with .git",
			url:       "https://github.com/fini-net/gh-observer.git",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantErr:   false,
		},
		{
			name:      "HTTPS format without .git",
			url:       "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:      "HTTPS format with trailing slash",
			url:       "https://github.com/owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
		{
			name:    "invalid format",
			url:     "invalid-url",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:      "repo with hyphens and numbers",
			url:       "git@github.com:org-123/repo-name-456.git",
			wantOwner: "org-123",
			wantRepo:  "repo-name-456",
			wantErr:   false,
		},
		{
			name:      "HTTPS with repo containing dots",
			url:       "https://github.com/owner/repo.name.git",
			wantOwner: "owner",
			wantRepo:  "repo.name",
			wantErr:   false,
		},
		{
			name:      "SSH with trailing slash",
			url:       "git@github.com:owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotRepo, err := parseOwnerRepoFromURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseOwnerRepoFromURL(%q) expected error, got nil", tt.url)
				}
				return
			}
			if err != nil {
				t.Errorf("parseOwnerRepoFromURL(%q) unexpected error: %v", tt.url, err)
				return
			}
			if gotOwner != tt.wantOwner {
				t.Errorf("parseOwnerRepoFromURL(%q) owner = %q, want %q", tt.url, gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("parseOwnerRepoFromURL(%q) repo = %q, want %q", tt.url, gotRepo, tt.wantRepo)
			}
		})
	}
}

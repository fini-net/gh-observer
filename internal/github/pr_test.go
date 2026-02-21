package github

import (
	"testing"
)

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

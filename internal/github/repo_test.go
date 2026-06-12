package github

import (
	"testing"
)

func TestParseRepoArg(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			input:     "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:     "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:     "https://github.com/owner/repo.name",
			wantOwner: "owner",
			wantRepo:  "repo.name",
		},
		{
			input:     "http://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:   "",
			wantErr: true,
		},
		{
			input:   "owner",
			wantErr: true,
		},
		{
			input:   "owner/repo/extra",
			wantErr: true,
		},
		{
			input:   "https://gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			input:   "https://github.com/owner/repo/pull/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo, err := ParseRepoArg(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRepoArg(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("ParseRepoArg(%q) owner = %q, want %q", tt.input, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("ParseRepoArg(%q) repo = %q, want %q", tt.input, repo, tt.wantRepo)
			}
		})
	}
}
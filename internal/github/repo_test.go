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
			input:     "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:     "https://github.com/owner/repo.git/",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			// Embedded underscores are allowed in GitHub owner/repo names.
			input:     "my_org/my_repo",
			wantOwner: "my_org",
			wantRepo:  "my_repo",
		},
		{
			input:     "https://github.com/my_org/my_repo",
			wantOwner: "my_org",
			wantRepo:  "my_repo",
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
		{
			input:   "https://github.com/owner/repo/actions/runs/456",
			wantErr: true,
		},
		// All-underscore segments are rejected so the --repo auto-detect
		// sentinel "_" in main.go can't collide with a literal owner/repo.
		// These should error rather than being treated as auto-detect or as
		// a real repo named "_".
		{
			input:   "_",
			wantErr: true,
		},
		{
			input:   "__",
			wantErr: true,
		},
		{
			input:   "_/repo",
			wantErr: true,
		},
		{
			input:   "owner/_",
			wantErr: true,
		},
		{
			input:   "__/__",
			wantErr: true,
		},
		{
			input:   "https://github.com/_/repo",
			wantErr: true,
		},
		{
			input:   "https://github.com/owner/_",
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

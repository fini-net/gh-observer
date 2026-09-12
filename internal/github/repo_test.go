package github

import (
	"strings"
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

// FuzzParseRepoArg checks the owner/repo argument parser used by the --repo
// flag. Runs as a seed-corpus unit test under plain `go test ./...`; real
// fuzzing is opt-in via `just fuzz`. On success, both segments must be valid
// GitHub slugs and must never be composed entirely of underscores — "_" is
// the --repo auto-detect sentinel in main.go and must not parse as a literal
// owner/repo.
func FuzzParseRepoArg(f *testing.F) {
	seeds := []string{
		"owner/repo",
		"https://github.com/owner/repo",
		"https://github.com/owner/repo.name",
		"http://github.com/owner/repo",
		"https://github.com/owner/repo.git",
		"https://github.com/owner/repo.git/",
		"my_org/my_repo",
		"https://github.com/my_org/my_repo",
		"",
		"owner",
		"owner/repo/extra",
		"https://gitlab.com/owner/repo",
		"https://github.com/owner/repo/pull/123",
		"https://github.com/owner/repo/actions/runs/456",
		"_",
		"__",
		"_/repo",
		"owner/_",
		"__/__",
		"https://github.com/_/repo",
		"https://github.com/owner/_",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, arg string) {
		owner, repo, err := ParseRepoArg(arg)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "ParseRepoArg("+arg+")", owner, repo)
		if isAllUnderscoreSegment(owner) || isAllUnderscoreSegment(repo) {
			t.Errorf("ParseRepoArg(%q) accepted all-underscore segment (owner=%q, repo=%q)", arg, owner, repo)
		}
		// A plain "owner/repo" slug must reconstruct the input exactly;
		// URL forms may legitimately differ (.git suffix, trailing slash).
		if !strings.Contains(arg, "://") && owner+"/"+repo != arg {
			t.Errorf("ParseRepoArg(%q) segments %q + %q do not reconstruct input", arg, owner, repo)
		}
		// Whatever the input form, the parsed slugs must re-parse as a
		// valid slug argument and yield the same owner/repo.
		reOwner, reRepo, err := ParseRepoArg(owner + "/" + repo)
		if err != nil {
			t.Errorf("ParseRepoArg(%q): re-parsed %q/%q rejected: %v", arg, owner, repo, err)
		} else if reOwner != owner || reRepo != repo {
			t.Errorf("ParseRepoArg(%q): re-parse gave %q/%q, want %q/%q", arg, reOwner, reRepo, owner, repo)
		}
	})
}

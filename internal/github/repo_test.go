package github

import (
	"strings"
	"testing"
)

func TestParseRepoArg(t *testing.T) {
	tests := []struct {
		input     string
		wantHost  string
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
			wantHost:  "github.com",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:     "https://github.com/owner/repo.name",
			wantHost:  "github.com",
			wantOwner: "owner",
			wantRepo:  "repo.name",
		},
		{
			input:     "http://github.com/owner/repo",
			wantHost:  "github.com",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:     "https://github.com/owner/repo.git",
			wantHost:  "github.com",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			input:     "https://github.com/owner/repo.git/",
			wantHost:  "github.com",
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
			wantHost:  "github.com",
			wantOwner: "my_org",
			wantRepo:  "my_repo",
		},
		{
			// GitHub Enterprise forms (issue #479): host/owner/repo slug and URL.
			input:     "github.example.com/fini-net/gh-observer",
			wantHost:  "github.example.com",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
		},
		{
			input:     "https://github.example.com/fini-net/gh-observer",
			wantHost:  "github.example.com",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
		},
		{
			// Host is lowercased on capture.
			input:     "https://GitHub.Example.Com/owner/repo",
			wantHost:  "github.example.com",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			// Non-GitHub hosts parse; auth fails later with a helpful error.
			input:     "https://gitlab.com/owner/repo",
			wantHost:  "gitlab.com",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			// 3 segments without a dot in the first can't be a host — the
			// host-slug branch requires a dot so this stays an error rather
			// than silently parsing "org" as a hostname.
			input:   "org/team/repo",
			wantErr: true,
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
		{
			input:   "github.example.com/_/repo",
			wantErr: true,
		},
		{
			input:   "github.example.com/owner/_",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, owner, repo, err := ParseRepoArg(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRepoArg(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if host != tt.wantHost {
				t.Errorf("ParseRepoArg(%q) host = %q, want %q", tt.input, host, tt.wantHost)
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
		"github.example.com/fini-net/gh-observer",
		"https://github.example.com/fini-net/gh-observer",
		"https://GitHub.Example.Com/owner/repo",
		"",
		"owner",
		"org/team/repo",
		"owner/repo/extra",
		"https://github.com/owner/repo/pull/123",
		"https://github.com/owner/repo/actions/runs/456",
		"_",
		"__",
		"_/repo",
		"owner/_",
		"__/__",
		"https://github.com/_/repo",
		"https://github.com/owner/_",
		"github.example.com/_/repo",
		"github.example.com/owner/_",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, arg string) {
		host, owner, repo, err := ParseRepoArg(arg)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "ParseRepoArg("+arg+")", owner, repo)
		if isAllUnderscoreSegment(owner) || isAllUnderscoreSegment(repo) {
			t.Errorf("ParseRepoArg(%q) accepted all-underscore segment (owner=%q, repo=%q)", arg, owner, repo)
		}
		if host != "" {
			assertValidHost(t, "ParseRepoArg("+arg+")", host)
		}
		// A plain "owner/repo" slug must reconstruct the input exactly;
		// URL and host-slug forms may legitimately differ (.git suffix,
		// trailing slash, lowercased host).
		if !strings.Contains(arg, "://") && strings.Count(arg, "/") == 1 && owner+"/"+repo != arg {
			t.Errorf("ParseRepoArg(%q) segments %q + %q do not reconstruct input", arg, owner, repo)
		}
		// Whatever the input form, the parsed slugs must re-parse as a
		// valid slug argument and yield the same owner/repo.
		reHost, reOwner, reRepo, err := ParseRepoArg(owner + "/" + repo)
		if err != nil {
			t.Errorf("ParseRepoArg(%q): re-parsed %q/%q rejected: %v", arg, owner, repo, err)
		} else if reHost != "" || reOwner != owner || reRepo != repo {
			t.Errorf("ParseRepoArg(%q): re-parse gave %q/%q/%q, want \"\"/%q/%q", arg, reHost, reOwner, reRepo, owner, repo)
		}
	})
}

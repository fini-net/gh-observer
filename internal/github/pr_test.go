package github

import (
	"regexp"
	"strconv"
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
		{
			name:    "deceptive string with github.com and /pull/",
			url:     "notes-about-github.com-api-/pull/123.md",
			wantErr: true,
		},
		{
			name:    "URL with extra path segments before pull",
			url:     "https://github.com/owner/repo/blob/main/pull/123",
			wantErr: true,
		},
		{
			name:    "URL with query params",
			url:     "https://github.com/owner/repo/pull/123?w=1",
			wantErr: true,
		},
		{
			name:    "URL with hash fragment",
			url:     "https://github.com/owner/repo/pull/123#issuecomment",
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

func TestParseActionsRunURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantRunID int64
		wantErr   bool
	}{
		{
			name:      "valid HTTPS URL",
			url:       "https://github.com/fini-net/gh-observer/actions/runs/25856656092",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantRunID: 25856656092,
			wantErr:   false,
		},
		{
			name:      "valid HTTP URL",
			url:       "http://github.com/owner/repo/actions/runs/123",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRunID: 123,
			wantErr:   false,
		},
		{
			name:      "owner with hyphens and numbers",
			url:       "https://github.com/org-123/repo-name/actions/runs/456",
			wantOwner: "org-123",
			wantRepo:  "repo-name",
			wantRunID: 456,
			wantErr:   false,
		},
		{
			name:      "repo with dots",
			url:       "https://github.com/owner/repo.name/actions/runs/789",
			wantOwner: "owner",
			wantRepo:  "repo.name",
			wantRunID: 789,
			wantErr:   false,
		},
		{
			name:    "missing protocol",
			url:     "github.com/owner/repo/actions/runs/123",
			wantErr: true,
		},
		{
			name:    "wrong host",
			url:     "https://gitlab.com/owner/repo/actions/runs/123",
			wantErr: true,
		},
		{
			name:    "wrong path format - pull instead of actions",
			url:     "https://github.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "missing run ID",
			url:     "https://github.com/owner/repo/actions/runs/",
			wantErr: true,
		},
		{
			name:    "non-numeric run ID",
			url:     "https://github.com/owner/repo/actions/runs/abc",
			wantErr: true,
		},
		{
			name:    "trailing slash",
			url:     "https://github.com/owner/repo/actions/runs/123/",
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
		{
			name:    "URL with query params",
			url:     "https://github.com/owner/repo/actions/runs/123?w=1",
			wantErr: true,
		},
		{
			name:    "URL with hash fragment",
			url:     "https://github.com/owner/repo/actions/runs/123#summary",
			wantErr: true,
		},
		{
			name:    "URL with job path (should not match)",
			url:     "https://github.com/owner/repo/actions/runs/123/job/456",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotRepo, gotRunID, err := ParseActionsRunURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseActionsRunURL(%q) expected error, got nil", tt.url)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseActionsRunURL(%q) unexpected error: %v", tt.url, err)
				return
			}
			if gotOwner != tt.wantOwner {
				t.Errorf("ParseActionsRunURL(%q) owner = %q, want %q", tt.url, gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("ParseActionsRunURL(%q) repo = %q, want %q", tt.url, gotRepo, tt.wantRepo)
			}
			if gotRunID != tt.wantRunID {
				t.Errorf("ParseActionsRunURL(%q) runID = %d, want %d", tt.url, gotRunID, tt.wantRunID)
			}
		})
	}
}

// Go native fuzz targets for the untrusted-input parsers. Seed corpora come
// from the table tests above plus adversarial shapes (deeply nested JSON,
// oversized numeric literals, malformed UTF-8). Under plain `go test ./...`
// (what CI runs) each target executes only its seed corpus as ordinary unit
// tests, so these act as permanent regression tests at zero workflow cost.
// Coverage-guided fuzzing is opt-in via `just fuzz`.
//
// The invariants assert that on success the parsed owner/repo are GitHub
// slugs, the numeric ID is positive, and — for the gh CLI JSON path — that
// the canonical URL rebuilt from the parsed fields re-parses identically.

var slugRE = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// assertValidOwnerRepo fails t unless owner and repo are non-empty GitHub slugs.
func assertValidOwnerRepo(t *testing.T, label, owner, repo string) {
	t.Helper()
	if !slugRE.MatchString(owner) {
		t.Errorf("%s: owner %q is not a GitHub slug", label, owner)
	}
	if !slugRE.MatchString(repo) {
		t.Errorf("%s: repo %q is not a GitHub slug", label, repo)
	}
}

func FuzzParsePRURL(f *testing.F) {
	seeds := []string{
		"https://github.com/fini-net/gh-observer/pull/88",
		"http://github.com/owner/repo/pull/123",
		"https://github.com/org-123/repo-name/pull/456",
		"https://github.com/owner/repo.name/pull/789",
		"github.com/owner/repo/pull/123",
		"https://gitlab.com/owner/repo/pull/123",
		"https://github.com/owner/repo/issues/123",
		"https://github.com/owner/repo/pull/",
		"https://github.com/owner/repo/pull/abc",
		"https://github.com/owner/repo/pull/123/",
		"",
		"https://github.com",
		"notes-about-github.com-api-/pull/123.md",
		"https://github.com/owner/repo/blob/main/pull/123",
		"https://github.com/owner/repo/pull/123?w=1",
		"https://github.com/owner/repo/pull/123#issuecomment",
		"https://github.com/owner/repo/pull/0",
		"https://github.com/owner/repo/pull/-1",
		"https://github.com/owner/repo/pull/99999999999999999999",
		"https://github.com/owner/repo/pull/007",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, url string) {
		owner, repo, prNum, err := ParsePRURL(url)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "ParsePRURL("+url+")", owner, repo)
		if prNum <= 0 {
			t.Errorf("ParsePRURL(%q) accepted non-positive PR number %d", url, prNum)
		}
	})
}

func FuzzParseActionsRunURL(f *testing.F) {
	seeds := []string{
		"https://github.com/fini-net/gh-observer/actions/runs/25856656092",
		"http://github.com/owner/repo/actions/runs/123",
		"https://github.com/org-123/repo-name/actions/runs/456",
		"https://github.com/owner/repo.name/actions/runs/789",
		"github.com/owner/repo/actions/runs/123",
		"https://gitlab.com/owner/repo/actions/runs/123",
		"https://github.com/owner/repo/pull/123",
		"https://github.com/owner/repo/actions/runs/",
		"https://github.com/owner/repo/actions/runs/abc",
		"https://github.com/owner/repo/actions/runs/123/",
		"",
		"https://github.com",
		"https://github.com/owner/repo/actions/runs/123?w=1",
		"https://github.com/owner/repo/actions/runs/123#summary",
		"https://github.com/owner/repo/actions/runs/123/job/456",
		"https://github.com/owner/repo/actions/runs/0",
		"https://github.com/owner/repo/actions/runs/9223372036854775808",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, url string) {
		owner, repo, runID, err := ParseActionsRunURL(url)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "ParseActionsRunURL("+url+")", owner, repo)
		if runID <= 0 {
			t.Errorf("ParseActionsRunURL(%q) accepted non-positive run ID %d", url, runID)
		}
	})
}

func FuzzParsePRViewWithRepo(f *testing.F) {
	seeds := []string{
		`{"number":4173,"url":"https://github.com/StackExchange/dnscontrol/pull/4173"}`,
		`{"number":123,"url":"https://github.com/upstream-owner/upstream-repo/pull/123"}`,
		`{"number":456,"url":"https://github.com/org-123/repo-name-789/pull/456"}`,
		`{"number":1,"url":"https://github.com/owner/repo.name/pull/1"}`,
		`{"url":"https://github.com/owner/repo/pull/123"}`,
		`{"number":123}`,
		`{"number":123,"url":"https://github.com/owner/repo/issues/123"}`,
		`{invalid json`,
		`{}`,
		`{"number":123,"url":"https://github.com/owner/repo/pull/456"}`,
		`{"number":-5,"url":"https://github.com/owner/repo/pull/123"}`,
		`{"number":1e3,"url":"https://github.com/owner/repo/pull/1000"}`,
		`{"number":123,"url":"https://github.com/owner/repo/pull/123","extra":{"deeply":{"nested":[1,2,{"x":true}]}}}`,
		`null`,
		`[]`,
		`"just a string"`,
		`{"number":123,"url":null}`,
		`{"number":123,"url":123}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, jsonOutput []byte) {
		number, owner, repo, err := parsePRViewWithRepo(jsonOutput)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "parsePRViewWithRepo("+string(jsonOutput)+")", owner, repo)
		if number <= 0 {
			t.Errorf("parsePRViewWithRepo(%q) accepted non-positive PR number %d", jsonOutput, number)
		}
		// The parsed triple must be self-consistent: the canonical URL
		// rebuilt from owner/repo/number has to re-parse identically.
		canonical := "https://github.com/" + owner + "/" + repo + "/pull/" + strconv.Itoa(number)
		cOwner, cRepo, cNum, err := ParsePRURL(canonical)
		if err != nil {
			t.Fatalf("parsePRViewWithRepo(%q): canonical URL %q failed to re-parse: %v",
				jsonOutput, canonical, err)
		}
		if cNum != number || cOwner != owner || cRepo != repo {
			t.Errorf("parsePRViewWithRepo(%q) round-trip mismatch: got (%d, %q, %q), want (%d, %q, %q)",
				jsonOutput, cNum, cOwner, cRepo, number, owner, repo)
		}
	})
}

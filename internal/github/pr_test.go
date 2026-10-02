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
		wantHost  string
		wantOwner string
		wantRepo  string
		wantPRNum int
		wantErr   bool
	}{
		{
			name:      "valid HTTPS URL",
			url:       "https://github.com/fini-net/gh-observer/pull/88",
			wantHost:  "github.com",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantPRNum: 88,
			wantErr:   false,
		},
		{
			name:      "valid HTTP URL",
			url:       "http://github.com/owner/repo/pull/123",
			wantHost:  "github.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPRNum: 123,
			wantErr:   false,
		},
		{
			name:      "enterprise URL",
			url:       "https://github.example.com/fini-net/gh-observer/pull/1",
			wantHost:  "github.example.com",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantPRNum: 1,
			wantErr:   false,
		},
		{
			name:      "enterprise URL host lowercased",
			url:       "https://GitHub.Example.Com/owner/repo/pull/7",
			wantHost:  "github.example.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPRNum: 7,
			wantErr:   false,
		},
		{
			name:      "non-GitHub host parses (auth fails later)",
			url:       "https://gitlab.com/owner/repo/pull/123",
			wantHost:  "gitlab.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPRNum: 123,
			wantErr:   false,
		},
		{
			name:      "owner with hyphens and numbers",
			url:       "https://github.com/org-123/repo-name/pull/456",
			wantHost:  "github.com",
			wantOwner: "org-123",
			wantRepo:  "repo-name",
			wantPRNum: 456,
			wantErr:   false,
		},
		{
			name:      "repo with dots",
			url:       "https://github.com/owner/repo.name/pull/789",
			wantHost:  "github.com",
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
		{
			name:    "host with port rejected",
			url:     "https://github.example.com:8443/owner/repo/pull/1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotOwner, gotRepo, gotPRNum, err := ParsePRURL(tt.url)
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
			if gotHost != tt.wantHost {
				t.Errorf("ParsePRURL(%q) host = %q, want %q", tt.url, gotHost, tt.wantHost)
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
		wantHost   string
		wantOwner  string
		wantRepo   string
		wantErr    bool
		errContain string
	}{
		{
			name:       "valid PR view output",
			jsonInput:  `{"number":4173,"url":"https://github.com/StackExchange/dnscontrol/pull/4173"}`,
			wantNumber: 4173,
			wantHost:   "github.com",
			wantOwner:  "StackExchange",
			wantRepo:   "dnscontrol",
			wantErr:    false,
		},
		{
			name:       "fork scenario - upstream repo in URL",
			jsonInput:  `{"number":123,"url":"https://github.com/upstream-owner/upstream-repo/pull/123"}`,
			wantNumber: 123,
			wantHost:   "github.com",
			wantOwner:  "upstream-owner",
			wantRepo:   "upstream-repo",
			wantErr:    false,
		},
		{
			name:       "enterprise PR view output",
			jsonInput:  `{"number":1,"url":"https://github.example.com/fini-net/gh-observer/pull/1"}`,
			wantNumber: 1,
			wantHost:   "github.example.com",
			wantOwner:  "fini-net",
			wantRepo:   "gh-observer",
			wantErr:    false,
		},
		{
			name:       "owner with hyphens and numbers",
			jsonInput:  `{"number":456,"url":"https://github.com/org-123/repo-name-789/pull/456"}`,
			wantNumber: 456,
			wantHost:   "github.com",
			wantOwner:  "org-123",
			wantRepo:   "repo-name-789",
			wantErr:    false,
		},
		{
			name:       "repo with dots",
			jsonInput:  `{"number":1,"url":"https://github.com/owner/repo.name/pull/1"}`,
			wantNumber: 1,
			wantHost:   "github.com",
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
			gotNumber, gotHost, gotOwner, gotRepo, err := parsePRViewWithRepo([]byte(tt.jsonInput))
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
			if gotHost != tt.wantHost {
				t.Errorf("parsePRViewWithRepo(%q) host = %q, want %q", tt.jsonInput, gotHost, tt.wantHost)
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
		wantHost  string
		wantOwner string
		wantRepo  string
		wantRunID int64
		wantErr   bool
	}{
		{
			name:      "valid HTTPS URL",
			url:       "https://github.com/fini-net/gh-observer/actions/runs/25856656092",
			wantHost:  "github.com",
			wantOwner: "fini-net",
			wantRepo:  "gh-observer",
			wantRunID: 25856656092,
			wantErr:   false,
		},
		{
			name:      "valid HTTP URL",
			url:       "http://github.com/owner/repo/actions/runs/123",
			wantHost:  "github.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRunID: 123,
			wantErr:   false,
		},
		{
			name:      "enterprise URL",
			url:       "https://github.example.com/owner/repo/actions/runs/456",
			wantHost:  "github.example.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRunID: 456,
			wantErr:   false,
		},
		{
			name:      "enterprise URL host lowercased",
			url:       "https://GitHub.Example.Com/owner/repo/actions/runs/456",
			wantHost:  "github.example.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRunID: 456,
			wantErr:   false,
		},
		{
			name:      "non-GitHub host parses (auth fails later)",
			url:       "https://gitlab.com/owner/repo/actions/runs/123",
			wantHost:  "gitlab.com",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRunID: 123,
			wantErr:   false,
		},
		{
			name:      "owner with hyphens and numbers",
			url:       "https://github.com/org-123/repo-name/actions/runs/456",
			wantHost:  "github.com",
			wantOwner: "org-123",
			wantRepo:  "repo-name",
			wantRunID: 456,
			wantErr:   false,
		},
		{
			name:      "repo with dots",
			url:       "https://github.com/owner/repo.name/actions/runs/789",
			wantHost:  "github.com",
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
		{
			name:    "host with port rejected",
			url:     "https://github.example.com:8443/owner/repo/actions/runs/1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotOwner, gotRepo, gotRunID, err := ParseActionsRunURL(tt.url)
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
			if gotHost != tt.wantHost {
				t.Errorf("ParseActionsRunURL(%q) host = %q, want %q", tt.url, gotHost, tt.wantHost)
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

// hostRE matches the hostname shape accepted by the URL/remote parsers
// (mirrors host.go's defaultHostRegex, including single-char hosts).
var hostRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$|^[a-z0-9]$`)

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

// assertValidHost fails t unless host is a non-empty, lowercased hostname.
func assertValidHost(t *testing.T, label, host string) {
	t.Helper()
	if host == "" {
		t.Errorf("%s: host is empty", label)
		return
	}
	if host != strings.ToLower(host) {
		t.Errorf("%s: host %q is not lowercased", label, host)
	}
	if !hostRE.MatchString(host) {
		t.Errorf("%s: host %q is not a hostname shape", label, host)
	}
}

func FuzzParsePRURL(f *testing.F) {
	seeds := []string{
		"https://github.com/fini-net/gh-observer/pull/88",
		"http://github.com/owner/repo/pull/123",
		"https://github.com/org-123/repo-name/pull/456",
		"https://github.com/owner/repo.name/pull/789",
		"https://github.example.com/fini-net/gh-observer/pull/1",
		"https://GitHub.Example.Com/owner/repo/pull/7",
		"https://gitlab.com/owner/repo/pull/123",
		"github.com/owner/repo/pull/123",
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
		"https://github.example.com:8443/owner/repo/pull/1",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, url string) {
		host, owner, repo, prNum, err := ParsePRURL(url)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "ParsePRURL("+url+")", owner, repo)
		assertValidHost(t, "ParsePRURL("+url+")", host)
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
		"https://github.example.com/owner/repo/actions/runs/456",
		"https://gitlab.com/owner/repo/actions/runs/123",
		"github.com/owner/repo/actions/runs/123",
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
		"https://github.example.com:8443/owner/repo/actions/runs/1",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, url string) {
		host, owner, repo, runID, err := ParseActionsRunURL(url)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "ParseActionsRunURL("+url+")", owner, repo)
		assertValidHost(t, "ParseActionsRunURL("+url+")", host)
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
		`{"number":1,"url":"https://github.example.com/fini-net/gh-observer/pull/1"}`,
		`{"number":1,"url":"https://GitHub.Example.Com/owner/repo/pull/7"}`,
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
		number, host, owner, repo, err := parsePRViewWithRepo(jsonOutput)
		if err != nil {
			return
		}
		assertValidOwnerRepo(t, "parsePRViewWithRepo("+string(jsonOutput)+")", owner, repo)
		assertValidHost(t, "parsePRViewWithRepo("+string(jsonOutput)+")", host)
		if number <= 0 {
			t.Errorf("parsePRViewWithRepo(%q) accepted non-positive PR number %d", jsonOutput, number)
		}
		// The parsed fields must be self-consistent: the canonical URL
		// rebuilt from host/owner/repo/number has to re-parse identically.
		canonical := "https://" + host + "/" + owner + "/" + repo + "/pull/" + strconv.Itoa(number)
		cHost, cOwner, cRepo, cNum, err := ParsePRURL(canonical)
		if err != nil {
			t.Fatalf("parsePRViewWithRepo(%q): canonical URL %q failed to re-parse: %v",
				jsonOutput, canonical, err)
		}
		if cNum != number || cOwner != owner || cRepo != repo || cHost != host {
			t.Errorf("parsePRViewWithRepo(%q) round-trip mismatch: got (%q, %q, %q, %d), want (%q, %q, %q, %d)",
				jsonOutput, cHost, cOwner, cRepo, cNum, host, owner, repo, number)
		}
	})
}

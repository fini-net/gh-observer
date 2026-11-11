package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/spf13/cobra"
)

func TestParseArgsPRURL(t *testing.T) {
	parsed, err := parseArgs([]string{"https://github.com/owner/repo/pull/123"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modePR {
		t.Errorf("mode = %v, want modePR", parsed.mode)
	}
	if parsed.host != "github.com" {
		t.Errorf("host = %q, want github.com", parsed.host)
	}
	if parsed.owner != "owner" || parsed.repo != "repo" || parsed.prNumber != 123 {
		t.Errorf("parsed = %+v, want owner/repo/123", parsed)
	}
}

func TestParseArgsEnterprisePRURL(t *testing.T) {
	parsed, err := parseArgs([]string{"https://github.example.com/fini-net/gh-observer/pull/1"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modePR {
		t.Errorf("mode = %v, want modePR", parsed.mode)
	}
	if parsed.host != "github.example.com" {
		t.Errorf("host = %q, want github.example.com", parsed.host)
	}
	if parsed.owner != "fini-net" || parsed.repo != "gh-observer" || parsed.prNumber != 1 {
		t.Errorf("parsed = %+v, want fini-net/gh-observer/1", parsed)
	}
}

func TestParseArgsActionsRunURL(t *testing.T) {
	parsed, err := parseArgs([]string{"https://github.com/owner/repo/actions/runs/987654321"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modeRun {
		t.Errorf("mode = %v, want modeRun", parsed.mode)
	}
	if parsed.host != "github.com" {
		t.Errorf("host = %q, want github.com", parsed.host)
	}
	if parsed.owner != "owner" || parsed.repo != "repo" || parsed.runID != 987654321 {
		t.Errorf("parsed = %+v, want owner/repo/987654321", parsed)
	}
}

func TestParseArgsEnterpriseActionsRunURL(t *testing.T) {
	parsed, err := parseArgs([]string{"https://github.example.com/owner/repo/actions/runs/456"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modeRun {
		t.Errorf("mode = %v, want modeRun", parsed.mode)
	}
	if parsed.host != "github.example.com" {
		t.Errorf("host = %q, want github.example.com", parsed.host)
	}
	if parsed.owner != "owner" || parsed.repo != "repo" || parsed.runID != 456 {
		t.Errorf("parsed = %+v, want owner/repo/456", parsed)
	}
}

func TestParseArgsInvalidArgument(t *testing.T) {
	_, err := parseArgs([]string{"not-a-valid-argument"})
	if err == nil {
		t.Fatal("parseArgs() should reject non-URL, non-numeric arguments")
	}
}

// fakeGhOnPath installs a stub `gh` binary on PATH that prints canned
// 'gh pr view --json number,url' JSON, then restores PATH on cleanup.
// The script only uses shell builtins (printf) so it works even though
// PATH contains nothing but the stub's directory.
func fakeGhOnPath(t *testing.T, jsonOutput string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + jsonOutput + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir)
}

// fakeGhFailsOnPath installs a stub `gh` that always exits 1, for the
// error paths that depend on gh CLI failure (PR detection, token
// resolution). Like fakeGhOnPath it only uses shell builtins, so it
// works with a PATH containing nothing but the stub's directory.
func fakeGhFailsOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write failing fake gh: %v", err)
	}
	t.Setenv("PATH", dir)
}

// TestParseArgsNumericPRNumber guards the field-order contract between
// GetPRWithRepo and parseArgs: the host must land in runArgs.host, not the
// repo name (a regression shipped with the GHE host threading when the
// return tuple was destructured in the wrong order — "authentication failed
// for gh-observer").
func TestParseArgsNumericPRNumber(t *testing.T) {
	fakeGhOnPath(t, `{"number":42,"url":"https://github.com/owner/repo/pull/42"}`)

	parsed, err := parseArgs([]string{"42"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modePR {
		t.Errorf("mode = %v, want modePR", parsed.mode)
	}
	if parsed.host != "github.com" {
		t.Errorf("host = %q, want github.com (repo name leaked into host?)", parsed.host)
	}
	if parsed.owner != "owner" || parsed.repo != "repo" || parsed.prNumber != 42 {
		t.Errorf("parsed = %+v, want owner/repo/42", parsed)
	}
}

// TestParseArgsNumericPRNumberEnterprise checks the same contract for an
// enterprise URL returned by 'gh pr view' inside an enterprise checkout.
func TestParseArgsNumericPRNumberEnterprise(t *testing.T) {
	fakeGhOnPath(t, `{"number":1,"url":"https://github.example.com/fini-net/gh-observer/pull/1"}`)

	parsed, err := parseArgs([]string{"1"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modePR {
		t.Errorf("mode = %v, want modePR", parsed.mode)
	}
	if parsed.host != "github.example.com" {
		t.Errorf("host = %q, want github.example.com", parsed.host)
	}
	if parsed.owner != "fini-net" || parsed.repo != "gh-observer" || parsed.prNumber != 1 {
		t.Errorf("parsed = %+v, want fini-net/gh-observer/1", parsed)
	}
}

func TestParseArgsSingleArgContract(t *testing.T) {
	// parseArgs only inspects args[0]; cobra's MaximumNArgs(1) handles the
	// count at the CLI layer, so a single-element slice is the contract.
	// Here we verify the first element still parses when called directly.
	parsed, err := parseArgs([]string{"https://github.com/owner/repo/pull/5"})
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.prNumber != 5 {
		t.Errorf("prNumber = %d, want 5", parsed.prNumber)
	}
}

// TestParseArgsNoArgsAutoDetect guards the zero-argument contract: with
// no positional, the PR and its host come from 'gh pr view --json
// number,url' — the same stubbed command the numeric path uses.
func TestParseArgsNoArgsAutoDetect(t *testing.T) {
	fakeGhOnPath(t, `{"number":42,"url":"https://github.com/owner/repo/pull/42"}`)

	parsed, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs() error: %v", err)
	}
	if parsed.mode != modePR {
		t.Errorf("mode = %v, want modePR", parsed.mode)
	}
	if parsed.host != "github.com" || parsed.owner != "owner" || parsed.repo != "repo" || parsed.prNumber != 42 {
		t.Errorf("parsed = %+v, want owner/repo/42 on github.com", parsed)
	}
}

// TestParseArgsNoArgsDetectionFailure covers the auto-detect error path.
// Only the shared "failed to detect PR" prefix is asserted: the jj
// (Jujutsu) variant of the message differs, and jj detection is cached
// process-globally, so a jj colocated checkout must not fail this test.
func TestParseArgsNoArgsDetectionFailure(t *testing.T) {
	fakeGhFailsOnPath(t)

	_, err := parseArgs(nil)
	if err == nil {
		t.Fatal("parseArgs() should fail when gh cannot detect the PR")
	}
	if !strings.Contains(err.Error(), "failed to detect PR") {
		t.Errorf("error = %q, want a detection failure", err)
	}
}

// TestParseArgsNumericPRFetchFailure covers the 'gh pr view N' failure
// path when an explicit PR number is given.
func TestParseArgsNumericPRFetchFailure(t *testing.T) {
	fakeGhFailsOnPath(t)

	_, err := parseArgs([]string{"42"})
	if err == nil {
		t.Fatal("parseArgs() should fail when gh cannot view the PR")
	}
	if !strings.Contains(err.Error(), "failed to get PR #42") {
		t.Errorf("error = %q, want the PR fetch failure", err)
	}
}

func TestResolveRepoArgExplicitValue(t *testing.T) {
	// An explicit owner/repo must parse without hitting the git remote.
	host, owner, repo, err := resolveRepoArg("owner/repo")
	if err != nil {
		t.Fatalf("resolveRepoArg() error: %v", err)
	}
	if host != "" {
		t.Errorf("resolveRepoArg() host = %q, want empty (default host)", host)
	}
	if owner != "owner" || repo != "repo" {
		t.Errorf("resolveRepoArg() = %q/%q, want owner/repo", owner, repo)
	}
}

func TestResolveRepoArgExplicitURL(t *testing.T) {
	host, owner, repo, err := resolveRepoArg("https://github.com/fini-net/gh-observer")
	if err != nil {
		t.Fatalf("resolveRepoArg() error: %v", err)
	}
	if host != "github.com" {
		t.Errorf("resolveRepoArg() host = %q, want github.com", host)
	}
	if owner != "fini-net" || repo != "gh-observer" {
		t.Errorf("resolveRepoArg() = %q/%q, want fini-net/gh-observer", owner, repo)
	}
}

func TestResolveRepoArgEnterpriseForms(t *testing.T) {
	host, owner, repo, err := resolveRepoArg("github.example.com/fini-net/gh-observer")
	if err != nil {
		t.Fatalf("resolveRepoArg() host/owner/repo error: %v", err)
	}
	if host != "github.example.com" || owner != "fini-net" || repo != "gh-observer" {
		t.Errorf("resolveRepoArg() = %q/%q/%q, want github.example.com/fini-net/gh-observer", host, owner, repo)
	}

	host, owner, repo, err = resolveRepoArg("https://github.example.com/fini-net/gh-observer")
	if err != nil {
		t.Fatalf("resolveRepoArg() enterprise URL error: %v", err)
	}
	if host != "github.example.com" || owner != "fini-net" || repo != "gh-observer" {
		t.Errorf("resolveRepoArg() = %q/%q/%q, want github.example.com/fini-net/gh-observer", host, owner, repo)
	}
}

func TestResolveRepoArgInvalidValue(t *testing.T) {
	_, _, _, err := resolveRepoArg("not a valid repo arg!!")
	if err == nil {
		t.Fatal("resolveRepoArg() should reject unparseable values")
	}
}

// chdirToGitRepo creates a git repo (with origin set to remote when
// non-empty) in a temp dir and chdirs there for the test's duration,
// restoring the original working directory on cleanup. Never combine
// with t.Parallel(): the working directory is process-global, and the
// jj detector's sync.Once cache walks up from it.
func chdirToGitRepo(t *testing.T, remote string) {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if remote != "" {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", remote).CombinedOutput(); err != nil {
			t.Fatalf("git remote add origin %s: %v: %s", remote, err, out)
		}
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

// TestResolveRepoArgAutoDetect covers the git-remote detection branch:
// an empty value or the bare --repo sentinel resolves via
// `git remote get-url origin`, across the remote forms GetCurrentRepo
// parses (HTTPS, SSH, enterprise SSH).
func TestResolveRepoArgAutoDetect(t *testing.T) {
	t.Run("https remote", func(t *testing.T) {
		chdirToGitRepo(t, "https://github.com/owner/repo.git")

		host, owner, repo, err := resolveRepoArg("")
		if err != nil {
			t.Fatalf("resolveRepoArg() error: %v", err)
		}
		if host != "github.com" || owner != "owner" || repo != "repo" {
			t.Errorf("resolveRepoArg() = %q/%q/%q, want github.com/owner/repo", host, owner, repo)
		}
	})

	t.Run("ssh remote", func(t *testing.T) {
		chdirToGitRepo(t, "git@github.com:owner/repo.git")

		host, owner, repo, err := resolveRepoArg("")
		if err != nil {
			t.Fatalf("resolveRepoArg() error: %v", err)
		}
		if host != "github.com" || owner != "owner" || repo != "repo" {
			t.Errorf("resolveRepoArg() = %q/%q/%q, want github.com/owner/repo", host, owner, repo)
		}
	})

	t.Run("enterprise ssh remote", func(t *testing.T) {
		chdirToGitRepo(t, "git@ghe.example.com:owner/repo.git")

		host, owner, repo, err := resolveRepoArg("")
		if err != nil {
			t.Fatalf("resolveRepoArg() error: %v", err)
		}
		if host != "ghe.example.com" || owner != "owner" || repo != "repo" {
			t.Errorf("resolveRepoArg() = %q/%q/%q, want ghe.example.com/owner/repo", host, owner, repo)
		}
	})

	t.Run("sentinel behaves like empty", func(t *testing.T) {
		// A bare `--repo` leaves the sentinel in repoFlag; it must take
		// the auto-detect path, not the explicit-value path (which
		// would reject "_" as an all-underscore segment).
		chdirToGitRepo(t, "https://github.com/owner/repo.git")

		host, owner, repo, err := resolveRepoArg(repoFlagAutoSentinel)
		if err != nil {
			t.Fatalf("resolveRepoArg() error: %v", err)
		}
		if host != "github.com" || owner != "owner" || repo != "repo" {
			t.Errorf("resolveRepoArg() = %q/%q/%q, want github.com/owner/repo", host, owner, repo)
		}
	})

	t.Run("no remote errors with explicit-repo hint", func(t *testing.T) {
		chdirToGitRepo(t, "")

		_, _, _, err := resolveRepoArg("")
		if err == nil {
			t.Fatal("resolveRepoArg() should fail without a git remote")
		}
		if !strings.Contains(err.Error(), "failed to detect current repo") {
			t.Errorf("error = %q, want the detection failure", err)
		}
		if !strings.Contains(err.Error(), "Use --repo owner/repo") {
			t.Errorf("error = %q, want the explicit-repo hint", err)
		}
	})
}

// --- runSnapshot tests (issue #530) ---
//
// runSnapshot builds its clients from (token, host) — REST via
// NewClientFromToken, GraphQL inside FetchCheckRunsGraphQL — so tests can't
// inject an httptest server directly. The GH_OBSERVER_API_URL override
// (ghclient.APIURLEnvOverride, honored by APIURLsForHost for non-github.com
// hosts) redirects both endpoint derivations at a local httptest server via
// a fake enterprise host. REST requests arrive under /api/v3/ (go-github's
// WithEnterpriseURLs layout appends api/v3/ to hosts not under api.*);
// GraphQL at /graphql.

// snapshotHost is a fake enterprise host: never github.com (so the override
// applies), never a real hostname (so a misconfigured test without the
// override fails on connection, not on a real API call).
const snapshotHost = "ghe.example.com"

// snapshotPR is the REST pull-request fixture: FetchPRInfo reads number,
// title, and head.sha.
const snapshotPR = `{"number": 42, "title": "Add widgets", "state": "open", "head": {"sha": "abc123def456"}}`

// newSnapshotServer starts a fake GitHub API serving prJSON for the REST
// pull-request fetch and gqlJSON for the GraphQL check-runs query, then
// points GH_OBSERVER_API_URL at it. Empty prJSON → 404 (PR fetch failure);
// empty gqlJSON → 500 (check-runs fetch failure). The workflow-runs history
// list FetchJobAverages makes in quick=false mode always returns empty
// history. Any other route fails the test: runSnapshot should not touch it.
func newSnapshotServer(t *testing.T, prJSON, gqlJSON string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/graphql"):
			if r.Method != http.MethodPost {
				t.Errorf("GraphQL method = %s, want POST", r.Method)
			}
			if gqlJSON == "" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
				return
			}
			_, _ = w.Write([]byte(gqlJSON))
		case strings.Contains(r.URL.Path, "/pulls/"):
			if prJSON == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(prJSON))
		case strings.Contains(r.URL.Path, "/actions/workflows/"):
			// FetchJobAverages history list: empty history keeps the
			// averages path simple (no per-run job fetches).
			_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
		case strings.Contains(r.URL.Path, "/users/"):
			// CopilotReviewerExistsOnHost capability probe: 404 models an
			// enterprise host without the Copilot reviewer app (issue #442).
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			t.Errorf("unexpected request to fake API: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv(ghclient.APIURLEnvOverride, server.URL)
	return server
}

// fakeCheck describes one StatusCheckRollup CheckRun context node in the
// fake GraphQL response. Status/Conclusion take raw GraphQL-style uppercase
// values ("COMPLETED"/"SUCCESS"); zero StartedAt/CompletedAt are omitted,
// which leaves the corresponding CheckRunInfo pointers nil (queued checks).
type fakeCheck struct {
	Name        string
	Status      string
	Conclusion  string
	StartedAt   time.Time
	CompletedAt time.Time
	RunID       int64
	WorkflowID  int64
	Workflow    string
}

// buildChecksGraphQL builds the GraphQL response FetchCheckRunsGraphQL
// decodes: the head commit (pushedDate + status-check rollup contexts) and
// the pull request createdAt that clamps the cycle-start anchor (issue
// #518). Zero pushedAt/createdAt are omitted from the response, which the
// decoder reads as zero times. With zero pushedAt and no checks the commit
// list is empty — the zero-anchor no-checks shape.
func buildChecksGraphQL(t *testing.T, pushedAt, createdAt time.Time, checks []fakeCheck) string {
	t.Helper()
	nodes := make([]any, 0, len(checks))
	for _, c := range checks {
		node := map[string]any{
			"__typename":  "CheckRun",
			"name":        c.Name,
			"status":      c.Status,
			"conclusion":  c.Conclusion,
			"annotations": map[string]any{"nodes": []any{}},
			"checkSuite": map[string]any{
				"workflowRun": map[string]any{
					"databaseId": c.RunID,
					"workflow": map[string]any{
						"databaseId": c.WorkflowID,
						"name":       c.Workflow,
					},
				},
				"app": map[string]any{"name": "GitHub Actions", "slug": "github-actions"},
			},
		}
		if !c.StartedAt.IsZero() {
			node["startedAt"] = c.StartedAt.UTC().Format(time.RFC3339)
		}
		if !c.CompletedAt.IsZero() {
			node["completedAt"] = c.CompletedAt.UTC().Format(time.RFC3339)
		}
		nodes = append(nodes, node)
	}

	var commits []any
	if !pushedAt.IsZero() || len(checks) > 0 {
		commit := map[string]any{
			"statusCheckRollup": map[string]any{
				"contexts": map[string]any{
					"nodes":    nodes,
					"pageInfo": map[string]any{"hasNextPage": false},
				},
			},
		}
		if !pushedAt.IsZero() {
			commit["pushedDate"] = pushedAt.UTC().Format(time.RFC3339)
		}
		// Each commits.nodes entry is a CommitHistoryItem: the commit
		// object (pushedDate, statusCheckRollup) nests under "commit".
		commits = []any{map[string]any{"commit": commit}}
	}

	pr := map[string]any{"commits": map[string]any{"nodes": commits}}
	if !createdAt.IsZero() {
		pr["createdAt"] = createdAt.UTC().Format(time.RFC3339)
	}

	resp := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{"pullRequest": pr},
			"rateLimit":  map[string]any{"limit": 5000, "remaining": 4999},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal GraphQL fixture: %v", err)
	}
	return string(b)
}

// captureStdout runs fn with os.Stdout redirected into a pipe and returns
// everything fn wrote. runSnapshot prints its snapshot table straight to
// os.Stdout via fmt.Printf, so tests swap the fd to assert on the rendered
// output. Error paths write to os.Stderr, which passes through untouched.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r) // blocks until w is closed
		captured <- buf.String()
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	return <-captured
}

// callRunSnapshot runs the snapshot against the fake API and returns the
// exit code plus captured stdout. Links are disabled (plain text) and
// presumed averages are empty (interactive-mode concerns or separately
// covered); waitForCopilot toggles the Copilot review block.
func callRunSnapshot(t *testing.T, quick, waitForCopilot bool) (int, string) {
	t.Helper()
	var exitCode int
	output := captureStdout(t, func() {
		exitCode = runSnapshot(
			context.Background(), "test-token", snapshotHost, "owner", "repo", 42,
			false, quick, nil, waitForCopilot,
		)
	})
	return exitCode, output
}

func TestRunSnapshotNoChecks(t *testing.T) {
	t.Run("with cycle start prints started ago", func(t *testing.T) {
		// Push 30s ago, PR created 2h ago: the anchor is the later of the
		// two (issue #518), so the message must read as ~30s, not 2h.
		newSnapshotServer(t, snapshotPR,
			buildChecksGraphQL(t, time.Now().Add(-30*time.Second), time.Now().Add(-2*time.Hour), nil))

		code, out := callRunSnapshot(t, true, false)

		if code != 0 {
			t.Errorf("exitCode = %d, want 0 (no checks is not a failure)", code)
		}
		if !strings.Contains(out, "PR #42: Add widgets") {
			t.Errorf("output missing PR header, got:\n%s", out)
		}
		if !strings.Contains(out, "No checks found (started ") || !strings.Contains(out, " ago)") {
			t.Errorf("output missing 'No checks found (started X ago)', got:\n%s", out)
		}
		if strings.Contains(out, "2h") {
			t.Errorf("cycle start should be the push time, not PR creation (2h), got:\n%s", out)
		}
	})

	t.Run("without cycle start prints plain message", func(t *testing.T) {
		// No commits and no createdAt: zero anchor must not render a
		// "started X ago" suffix.
		newSnapshotServer(t, snapshotPR, buildChecksGraphQL(t, time.Time{}, time.Time{}, nil))

		code, out := callRunSnapshot(t, true, false)

		if code != 0 {
			t.Errorf("exitCode = %d, want 0", code)
		}
		if !strings.Contains(out, "No checks found") {
			t.Errorf("output missing 'No checks found', got:\n%s", out)
		}
		if strings.Contains(out, "(started") {
			t.Errorf("zero cycle start must not print a 'started' suffix, got:\n%s", out)
		}
		if !strings.Contains(out, "Checks may still be starting up or not configured for this PR") {
			t.Errorf("output missing hint line, got:\n%s", out)
		}
	})
}

func TestRunSnapshotAllChecksPass(t *testing.T) {
	now := time.Now()
	checks := []fakeCheck{
		{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute), RunID: 111, WorkflowID: 42, Workflow: "CI"},
		{Name: "lint", Status: "IN_PROGRESS", StartedAt: now.Add(-1 * time.Minute), RunID: 112, WorkflowID: 43, Workflow: "CI"},
		{Name: "e2e", Status: "QUEUED", RunID: 113, WorkflowID: 44, Workflow: "CI"},
	}
	newSnapshotServer(t, snapshotPR, buildChecksGraphQL(t, now.Add(-10*time.Minute), now.Add(-2*time.Hour), checks))

	// quick=false exercises the averages fetch path (empty history from the
	// fake API).
	code, out := callRunSnapshot(t, false, false)

	if code != 0 {
		t.Errorf("exitCode = %d, want 0 (all completed checks passed)", code)
	}
	if !strings.Contains(out, "PR #42: Add widgets") {
		t.Errorf("output missing PR header, got:\n%s", out)
	}
	for _, name := range []string{"build", "lint", "e2e"} {
		if !strings.Contains(out, name) {
			t.Errorf("output missing check %q, got:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("output missing success icon, got:\n%s", out)
	}
}

func TestRunSnapshotFailingCheck(t *testing.T) {
	now := time.Now()
	checks := []fakeCheck{
		{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute), RunID: 111, WorkflowID: 42, Workflow: "CI"},
		{Name: "test", Status: "COMPLETED", Conclusion: "FAILURE", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-7 * time.Minute), RunID: 114, WorkflowID: 45, Workflow: "CI"},
	}
	newSnapshotServer(t, snapshotPR, buildChecksGraphQL(t, now.Add(-10*time.Minute), now.Add(-2*time.Hour), checks))

	// quick=true also covers the averages-skip branch.
	code, out := callRunSnapshot(t, true, false)

	if code != 1 {
		t.Errorf("exitCode = %d, want 1 (a check failed)", code)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("output missing failed check 'test', got:\n%s", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("output missing failure icon, got:\n%s", out)
	}
}

func TestRunSnapshotFetchFailures(t *testing.T) {
	t.Run("PR info fetch failure exits 1", func(t *testing.T) {
		newSnapshotServer(t, "", buildChecksGraphQL(t, time.Now(), time.Time{}, nil))

		code, _ := callRunSnapshot(t, true, false)

		if code != 1 {
			t.Errorf("exitCode = %d, want 1 (PR fetch failed)", code)
		}
	})

	t.Run("check runs fetch failure exits 1", func(t *testing.T) {
		newSnapshotServer(t, snapshotPR, "")

		code, _ := callRunSnapshot(t, true, false)

		if code != 1 {
			t.Errorf("exitCode = %d, want 1 (check-runs fetch failed)", code)
		}
	})
}

func TestRunSnapshotCopilotNotAvailableOnHost(t *testing.T) {
	// waitForCopilot=true on a host whose capability probe 404s (the
	// issue #442 enterprise shape): the snapshot prints the explanatory
	// line instead of querying for reviews that cannot exist there, and
	// the exit code still reflects only the checks.
	now := time.Now()
	checks := []fakeCheck{
		{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute), RunID: 111, WorkflowID: 42, Workflow: "CI"},
	}
	newSnapshotServer(t, snapshotPR, buildChecksGraphQL(t, now.Add(-10*time.Minute), now.Add(-2*time.Hour), checks))

	code, out := callRunSnapshot(t, true, true)

	if code != 0 {
		t.Errorf("exitCode = %d, want 0 (an incapable host must not fail the snapshot)", code)
	}
	if !strings.Contains(out, "Copilot: not available on this host") {
		t.Errorf("output missing capability-probe line, got:\n%s", out)
	}
}

// --- runRunSnapshot tests (Actions run mode) ---
//
// Same fake-API approach as the runSnapshot suite above: the
// GH_OBSERVER_API_URL override redirects the REST run/jobs fetches
// (under /api/v3/, like the pull-request routes) and the GraphQL
// pushedDate lookup (at /graphql) at a local httptest server via the
// same fake enterprise host.

// snapshotRunID is the run ID every fixture serves: any /actions/runs/{id}
// request (the initial FetchRunInfo call, the FetchJobAverages workflow
// lookup, or the jobs list) gets the same canned run.
const snapshotRunID = int64(987)

// fakeRun describes the REST workflow-run fixture. Name exercises the
// DisplayTitle fallback while DisplayTitle overrides it (FetchRunInfo
// prefers display_title); HeadSHA gates the GraphQL pushedDate lookup;
// HeadCommitTime lands in head_commit.timestamp — the REST committer
// date kept as the push-time fallback when GraphQL yields nothing.
type fakeRun struct {
	Name           string
	DisplayTitle   string
	Status         string
	Conclusion     string
	WorkflowID     int64
	HeadSHA        string
	HeadCommitMsg  string
	HeadCommitTime time.Time
	CreatedAt      time.Time
	RunStartedAt   time.Time
}

// buildRunJSON builds the REST GetWorkflowRunByID response FetchRunInfo
// decodes. Empty strings and zero timestamps are omitted from the
// response, which leaves the corresponding RunInfo pointers nil.
func buildRunJSON(t *testing.T, run fakeRun) string {
	t.Helper()
	obj := map[string]any{"id": snapshotRunID, "status": run.Status}
	if run.Name != "" {
		obj["name"] = run.Name
	}
	if run.DisplayTitle != "" {
		obj["display_title"] = run.DisplayTitle
	}
	if run.Conclusion != "" {
		obj["conclusion"] = run.Conclusion
	}
	if run.WorkflowID != 0 {
		obj["workflow_id"] = run.WorkflowID
	}
	if run.HeadSHA != "" {
		obj["head_sha"] = run.HeadSHA
	}
	if run.HeadCommitMsg != "" || !run.HeadCommitTime.IsZero() {
		headCommit := map[string]any{}
		if run.HeadCommitMsg != "" {
			headCommit["message"] = run.HeadCommitMsg
		}
		if !run.HeadCommitTime.IsZero() {
			headCommit["timestamp"] = run.HeadCommitTime.UTC().Format(time.RFC3339)
		}
		obj["head_commit"] = headCommit
	}
	if !run.CreatedAt.IsZero() {
		obj["created_at"] = run.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !run.RunStartedAt.IsZero() {
		obj["run_started_at"] = run.RunStartedAt.UTC().Format(time.RFC3339)
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal run fixture: %v", err)
	}
	return string(b)
}

// fakeRunJob describes one job in the fake REST ListWorkflowJobs
// response. Status/Conclusion take REST-style lowercase values; zero
// StartedAt/CompletedAt are omitted (queued jobs).
type fakeRunJob struct {
	Name        string
	Workflow    string
	Status      string
	Conclusion  string
	StartedAt   time.Time
	CompletedAt time.Time
}

func buildRunJobsJSON(t *testing.T, jobs []fakeRunJob) string {
	t.Helper()
	nodes := make([]map[string]any, 0, len(jobs))
	for i, j := range jobs {
		node := map[string]any{
			"id":            i + 1,
			"run_id":        snapshotRunID,
			"name":          j.Name,
			"workflow_name": j.Workflow,
			"status":        j.Status,
			"conclusion":    j.Conclusion,
			"html_url":      fmt.Sprintf("https://ghe.example.com/owner/repo/actions/runs/%d/job/%d", snapshotRunID, i+1),
		}
		if !j.StartedAt.IsZero() {
			node["started_at"] = j.StartedAt.UTC().Format(time.RFC3339)
		}
		if !j.CompletedAt.IsZero() {
			node["completed_at"] = j.CompletedAt.UTC().Format(time.RFC3339)
		}
		nodes = append(nodes, node)
	}
	resp := map[string]any{"total_count": len(nodes), "jobs": nodes}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal jobs fixture: %v", err)
	}
	return string(b)
}

// buildPushedDateGraphQL builds the GraphQL response the head-commit
// pushedDate lookup (issue #349) decodes: pushedDate with a
// committedDate fallback. No __typename key — shurcooL/graphql's decoder
// fails on any response key without a matching query field, and the
// inline fragment's fields merge into the object map itself.
func buildPushedDateGraphQL(t *testing.T, pushedAt time.Time) string {
	t.Helper()
	object := map[string]any{}
	if !pushedAt.IsZero() {
		object["pushedDate"] = pushedAt.UTC().Format(time.RFC3339)
		object["committedDate"] = pushedAt.UTC().Format(time.RFC3339)
	}
	resp := map[string]any{
		"data": map[string]any{
			"repository": map[string]any{"object": object},
			"rateLimit":  map[string]any{"limit": 5000, "remaining": 4999},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal pushedDate fixture: %v", err)
	}
	return string(b)
}

// newRunSnapshotServer starts a fake GitHub API serving runJSON for the
// REST workflow-run fetch, jobsJSON for the jobs fetch, and gqlPushed
// for the head-commit pushedDate GraphQL lookup, then points
// GH_OBSERVER_API_URL at it. Empty runJSON → 404 (run fetch failure);
// empty jobsJSON → 500 (jobs fetch failure); empty gqlPushed → 500,
// which FetchRunInfo tolerates by falling back to head_commit.timestamp.
// The workflow-runs history list FetchJobAverages makes in quick=false
// mode always returns empty history. Any other route fails the test.
func newRunSnapshotServer(t *testing.T, runJSON, jobsJSON, gqlPushed string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/graphql"):
			if gqlPushed == "" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
				return
			}
			_, _ = w.Write([]byte(gqlPushed))
		case strings.Contains(r.URL.Path, "/actions/runs/"):
			// Run info and its jobs share this prefix; dispatch by
			// suffix. This route also serves the GetWorkflowRunByID
			// lookups FetchJobAverages makes per run ID in quick=false
			// mode.
			if strings.HasSuffix(r.URL.Path, "/jobs") {
				if jobsJSON == "" {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"message":"boom"}`))
					return
				}
				_, _ = w.Write([]byte(jobsJSON))
				return
			}
			if runJSON == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(runJSON))
		case strings.Contains(r.URL.Path, "/actions/workflows/"):
			// FetchJobAverages history list: empty history keeps the
			// averages path simple (no per-run job fetches).
			_, _ = w.Write([]byte(`{"total_count":0,"workflow_runs":[]}`))
		default:
			t.Errorf("unexpected request to fake API: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv(ghclient.APIURLEnvOverride, server.URL)
	return server
}

// callRunRunSnapshot runs the Actions-run snapshot against the fake API
// and returns the exit code plus captured stdout. Links are disabled
// (plain text) and presumed averages are empty, matching the PR-mode
// snapshot helper.
func callRunRunSnapshot(t *testing.T, quick bool) (int, string) {
	t.Helper()
	var exitCode int
	output := captureStdout(t, func() {
		exitCode = runRunSnapshot(
			context.Background(), "test-token", snapshotHost, "owner", "repo", snapshotRunID,
			false, quick, nil,
		)
	})
	return exitCode, output
}

func TestRunRunSnapshotNoJobs(t *testing.T) {
	t.Run("created-at fallback line", func(t *testing.T) {
		// No head_sha → the GraphQL pushedDate lookup is skipped; the
		// header falls back to created_at.
		newRunSnapshotServer(t,
			buildRunJSON(t, fakeRun{Name: "CI", Status: "in_progress", CreatedAt: time.Now().Add(-5 * time.Minute)}),
			buildRunJobsJSON(t, nil),
			buildPushedDateGraphQL(t, time.Now())) // never served: no head_sha

		code, out := callRunRunSnapshot(t, true)

		if code != 0 {
			t.Errorf("exitCode = %d, want 0 (no jobs is not a failure)", code)
		}
		if !strings.Contains(out, "owner/repo: CI") {
			t.Errorf("output missing run header, got:\n%s", out)
		}
		if !strings.Contains(out, "Created ") || !strings.Contains(out, " ago") {
			t.Errorf("output missing 'Created X ago', got:\n%s", out)
		}
		if strings.Contains(out, "Pushed ") {
			t.Errorf("created_at must be the fallback without head commit data, got:\n%s", out)
		}
		if !strings.Contains(out, "No jobs found") {
			t.Errorf("output missing 'No jobs found', got:\n%s", out)
		}
	})

	t.Run("no timestamps prints no time line", func(t *testing.T) {
		newRunSnapshotServer(t, buildRunJSON(t, fakeRun{Name: "CI", Status: "queued"}), buildRunJobsJSON(t, nil), "")

		code, out := callRunRunSnapshot(t, true)

		if code != 0 {
			t.Errorf("exitCode = %d, want 0", code)
		}
		if !strings.Contains(out, "owner/repo: CI") {
			t.Errorf("output missing run header, got:\n%s", out)
		}
		if strings.Contains(out, "ago") {
			t.Errorf("zero timestamps must not print a time line, got:\n%s", out)
		}
		if !strings.Contains(out, "No jobs found") {
			t.Errorf("output missing 'No jobs found', got:\n%s", out)
		}
	})
}

func TestRunRunSnapshotAllJobsPass(t *testing.T) {
	now := time.Now()
	run := fakeRun{
		Name:           "CI",
		DisplayTitle:   "Add widgets (#42)",
		Status:         "in_progress",
		WorkflowID:     42,
		HeadSHA:        "abc123def456",
		HeadCommitMsg:  "Add widgets",
		HeadCommitTime: now.Add(-2 * time.Hour), // REST fallback: must lose to GraphQL
		CreatedAt:      now.Add(-2 * time.Hour),
	}
	jobs := []fakeRunJob{
		{Name: "build", Workflow: "CI", Status: "completed", Conclusion: "success", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute)},
		{Name: "lint", Workflow: "CI", Status: "in_progress", StartedAt: now.Add(-1 * time.Minute)},
		{Name: "e2e", Workflow: "CI", Status: "queued"},
	}
	// GraphQL pushedDate 30s ago beats the 2h-old REST committer date.
	newRunSnapshotServer(t, buildRunJSON(t, run), buildRunJobsJSON(t, jobs), buildPushedDateGraphQL(t, now.Add(-30*time.Second)))

	// quick=false exercises the averages fetch path (per-run workflow
	// lookup + empty history from the fake API).
	code, out := callRunRunSnapshot(t, false)

	if code != 0 {
		t.Errorf("exitCode = %d, want 0 (no completed job failed)", code)
	}
	if !strings.Contains(out, "owner/repo: Add widgets (#42)") {
		t.Errorf("output missing run header, got:\n%s", out)
	}
	if !strings.Contains(out, "Pushed ") {
		t.Errorf("output missing 'Pushed X ago' from the GraphQL lookup, got:\n%s", out)
	}
	// The exact age varies by elapsed test time; the invariant is the
	// source: GraphQL's ~30s-old pushedDate, not the 2h REST fallback.
	if strings.Contains(out, "2h") {
		t.Errorf("GraphQL pushedDate must win over the REST head_commit fallback, got:\n%s", out)
	}
	for _, name := range []string{"build", "lint", "e2e"} {
		if !strings.Contains(out, name) {
			t.Errorf("output missing job %q, got:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("output missing success icon, got:\n%s", out)
	}
}

func TestRunRunSnapshotFailingJob(t *testing.T) {
	now := time.Now()
	run := fakeRun{
		Name:           "CI",
		Status:         "completed",
		Conclusion:     "failure",
		WorkflowID:     42,
		HeadSHA:        "abc123def456",
		HeadCommitTime: now.Add(-2 * time.Hour),
		CreatedAt:      now.Add(-2 * time.Hour),
	}
	jobs := []fakeRunJob{
		{Name: "build", Workflow: "CI", Status: "completed", Conclusion: "success", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute)},
		{Name: "test", Workflow: "CI", Status: "completed", Conclusion: "failure", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-7 * time.Minute)},
	}
	// GraphQL 500 (empty pushedDate fixture): FetchRunInfo falls back to
	// the REST head_commit timestamp, which must still render.
	newRunSnapshotServer(t, buildRunJSON(t, run), buildRunJobsJSON(t, jobs), "")

	// quick=true also covers the averages-skip branch.
	code, out := callRunRunSnapshot(t, true)

	if code != 1 {
		t.Errorf("exitCode = %d, want 1 (a job failed)", code)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("output missing failed job 'test', got:\n%s", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("output missing failure icon, got:\n%s", out)
	}
	if !strings.Contains(out, "Pushed 2h") {
		t.Errorf("output missing 'Pushed 2h...' from the REST fallback, got:\n%s", out)
	}
}

func TestRunRunSnapshotFetchFailures(t *testing.T) {
	t.Run("run info fetch failure exits 1", func(t *testing.T) {
		newRunSnapshotServer(t, "", buildRunJobsJSON(t, nil), "")

		code, _ := callRunRunSnapshot(t, true)

		if code != 1 {
			t.Errorf("exitCode = %d, want 1 (run fetch failed)", code)
		}
	})

	t.Run("jobs fetch failure exits 1", func(t *testing.T) {
		newRunSnapshotServer(t, buildRunJSON(t, fakeRun{Name: "CI", Status: "in_progress", CreatedAt: time.Now()}), "", "")

		code, _ := callRunRunSnapshot(t, true)

		if code != 1 {
			t.Errorf("exitCode = %d, want 1 (jobs fetch failed)", code)
		}
	})
}

// stubFrameModel is a minimal tea.Model with no FullView, for exercising
// printFinalFrame's fallback (issue #554).
type stubFrameModel struct {
	view string
}

func (m stubFrameModel) Init() tea.Cmd                       { return nil }
func (m stubFrameModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m stubFrameModel) View() tea.View                      { return tea.NewView(m.view) }

// stubFullFrameModel additionally implements FullView, like the scrolling
// PR and run modes.
type stubFullFrameModel struct {
	stubFrameModel
	fullView string
}

func (m stubFullFrameModel) FullView() string { return m.fullView }

// TestPrintFinalFrame locks in the quit-time scrollback contract: models
// implementing FullView (the scrolling PR and run modes) reprint their
// complete, unclipped body, while everything else (repo mode, error models)
// keeps printing View().Content.
func TestPrintFinalFrame(t *testing.T) {
	t.Run("full view preferred when implemented", func(t *testing.T) {
		m := stubFullFrameModel{
			stubFrameModel: stubFrameModel{view: "scrolled window\n"},
			fullView:       "entire body\n",
		}
		out := captureStdout(t, func() { printFinalFrame(m) })
		if out != "entire body\n" {
			t.Errorf("printFinalFrame = %q, want the FullView content", out)
		}
	})

	t.Run("view content fallback", func(t *testing.T) {
		m := stubFrameModel{view: "plain frame\n"}
		out := captureStdout(t, func() { printFinalFrame(m) })
		if out != "plain frame\n" {
			t.Errorf("printFinalFrame = %q, want the View content", out)
		}
	})
}

// --- run() tests ---
//
// run() is called directly: it returns an exit code, and only the cobra
// Run closure (never invoked here) calls os.Exit. The package-level
// flags run() reads are set per test and restored on cleanup, and the
// cobra command is a bare one whose only job is answering
// Flags().Changed("repo").

// runCmdForTest builds the bare cobra command run() consults for
// Flags().Changed("repo"); the flag's value is inert — run() reads the
// package-level globals, which tests set via withRunFlags.
func runCmdForTest(repoChanged bool) *cobra.Command {
	cmd := &cobra.Command{Use: "gh-observer"}
	cmd.Flags().String("repo", "", "")
	if repoChanged {
		_ = cmd.Flags().Set("repo", repoFlagAutoSentinel)
	}
	return cmd
}

// withRunFlags snapshots the package-level CLI flags run() reads and
// restores them on cleanup (run() mutates repoFlag when a bare --repo
// consumes a trailing positional). debugFlag is forced off: its branch
// installs a global slog handler that would outlive the test.
func withRunFlags(t *testing.T, repo string, quick bool) {
	t.Helper()
	oldRepo, oldQuick, oldDebug := repoFlag, quickFlag, debugFlag
	repoFlag, quickFlag, debugFlag = repo, quick, false
	t.Cleanup(func() { repoFlag, quickFlag, debugFlag = oldRepo, oldQuick, oldDebug })
}

// captureStderr is captureStdout's stderr counterpart: run()'s error
// paths write their user-facing messages to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()

	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r) // blocks until w is closed
		captured <- buf.String()
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	return <-captured
}

// TestRunRepoFlagValidation covers the --repo flag-combination guards.
// None of these reach the TUI: the terminal guard is exactly the
// condition a go test binary is always in, which is the point.
func TestRunRepoFlagValidation(t *testing.T) {
	t.Run("bare --repo consumes single positional", func(t *testing.T) {
		// `gh-observer --repo owner/repo`: pflag leaves the sentinel in
		// repoFlag and the positional in args; run() must fold the
		// positional into repoFlag before the guards.
		withRunFlags(t, repoFlagAutoSentinel, false)
		cmd := runCmdForTest(true)

		var code int
		stderr := captureStderr(t, func() { code = run(cmd, []string{"owner/repo"}) })

		if code != 1 {
			t.Errorf("exitCode = %d, want 1", code)
		}
		if repoFlag != "owner/repo" {
			t.Errorf("repoFlag = %q, want the consumed positional", repoFlag)
		}
		if !strings.Contains(stderr, "requires an interactive terminal") {
			t.Errorf("stderr missing terminal guard, got:\n%s", stderr)
		}
	})

	t.Run("--repo=value with positional rejected", func(t *testing.T) {
		withRunFlags(t, "owner/repo", false)
		cmd := runCmdForTest(true)

		var code int
		stderr := captureStderr(t, func() {
			code = run(cmd, []string{"https://github.com/owner/repo/pull/1"})
		})

		if code != 1 {
			t.Errorf("exitCode = %d, want 1", code)
		}
		if !strings.Contains(stderr, "cannot be used with positional arguments") {
			t.Errorf("stderr missing positional-args guard, got:\n%s", stderr)
		}
	})

	t.Run("--repo with --quick rejected", func(t *testing.T) {
		withRunFlags(t, "owner/repo", true)
		cmd := runCmdForTest(true)

		var code int
		stderr := captureStderr(t, func() { code = run(cmd, nil) })

		if code != 1 {
			t.Errorf("exitCode = %d, want 1", code)
		}
		if !strings.Contains(stderr, "cannot be used with --quick") {
			t.Errorf("stderr missing --quick guard, got:\n%s", stderr)
		}
	})

	t.Run("--repo requires interactive terminal", func(t *testing.T) {
		withRunFlags(t, "owner/repo", false)
		cmd := runCmdForTest(true)

		var code int
		stderr := captureStderr(t, func() { code = run(cmd, nil) })

		if code != 1 {
			t.Errorf("exitCode = %d, want 1", code)
		}
		if !strings.Contains(stderr, "requires an interactive terminal") {
			t.Errorf("stderr missing terminal guard, got:\n%s", stderr)
		}
	})
}

// TestRunConfigLoadFailure covers the config-load failure path: a
// config value that cannot decode into its target type fails viper's
// Unmarshal (its StringToTimeDurationHookFunc), which run() must
// surface as exit 1. HOME points at a temp dir so the suite never
// depends on (or mutates) the developer's real config.
func TestRunConfigLoadFailure(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "gh-observer")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("refresh_interval: not-a-duration\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HOME", home)

	withRunFlags(t, "", false)

	var code int
	stderr := captureStderr(t, func() { code = run(runCmdForTest(false), nil) })

	if code != 1 {
		t.Errorf("exitCode = %d, want 1 (config failed to load)", code)
	}
	if !strings.Contains(stderr, "Failed to load config") {
		t.Errorf("stderr missing load failure, got:\n%s", stderr)
	}
}

// TestRunInvalidArgument covers run()'s parseArgs error path.
func TestRunInvalidArgument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withRunFlags(t, "", false)

	var code int
	stderr := captureStderr(t, func() {
		code = run(runCmdForTest(false), []string{"not-a-valid-argument"})
	})

	if code != 1 {
		t.Errorf("exitCode = %d, want 1", code)
	}
	if !strings.Contains(stderr, "invalid PR number, PR URL, or Actions run URL") {
		t.Errorf("stderr missing parse failure, got:\n%s", stderr)
	}
}

// TestRunTokenFetchFailure covers the early token check on an
// enterprise host without credentials: env cleared and a failing gh
// stub on PATH (CI ships a real authenticated gh, so PATH stubbing is
// the only hermetic way to force the failure).
func TestRunTokenFetchFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		t.Setenv(name, "")
	}
	fakeGhFailsOnPath(t)
	withRunFlags(t, "", false)

	var code int
	stderr := captureStderr(t, func() {
		code = run(runCmdForTest(false), []string{"https://ghe.example.com/owner/repo/pull/42"})
	})

	if code != 1 {
		t.Errorf("exitCode = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Failed to get GitHub token") {
		t.Errorf("stderr missing token failure, got:\n%s", stderr)
	}
}

// TestRunPRModeSnapshot drives run() end to end through PR mode: go
// test never runs on a TTY, so runPRMode delegates to runSnapshot —
// one call covers run()'s spine (flag guards, config, parseArgs,
// token, mode switch) plus the snapshot delegation itself.
func TestRunPRModeSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GH_ENTERPRISE_TOKEN", "test-token")

	now := time.Now()
	checks := []fakeCheck{
		{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute), RunID: 111, WorkflowID: 42, Workflow: "CI"},
	}
	newSnapshotServer(t, snapshotPR, buildChecksGraphQL(t, now.Add(-10*time.Minute), now.Add(-2*time.Hour), checks))

	withRunFlags(t, "", false)

	var code int
	out := captureStdout(t, func() {
		code = run(runCmdForTest(false), []string{"https://ghe.example.com/owner/repo/pull/42"})
	})

	if code != 0 {
		t.Errorf("exitCode = %d, want 0 (the check passed)", code)
	}
	if !strings.Contains(out, "PR #42: Add widgets") {
		t.Errorf("output missing PR header, got:\n%s", out)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("output missing check name, got:\n%s", out)
	}
}

// TestRunActionsModeSnapshot does the same through Actions run mode,
// covering the modeRun switch arm and runActionsMode's snapshot
// delegation against the run-mode fake API.
func TestRunActionsModeSnapshot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GH_ENTERPRISE_TOKEN", "test-token")

	now := time.Now()
	runFixture := fakeRun{
		Name:           "CI",
		DisplayTitle:   "Add widgets (#42)",
		Status:         "in_progress",
		WorkflowID:     42,
		HeadSHA:        "abc123def456",
		HeadCommitTime: now.Add(-2 * time.Hour),
		CreatedAt:      now.Add(-2 * time.Hour),
	}
	jobs := []fakeRunJob{
		{Name: "build", Workflow: "CI", Status: "completed", Conclusion: "success", StartedAt: now.Add(-9 * time.Minute), CompletedAt: now.Add(-8 * time.Minute)},
	}
	newRunSnapshotServer(t, buildRunJSON(t, runFixture), buildRunJobsJSON(t, jobs), buildPushedDateGraphQL(t, now.Add(-30*time.Second)))

	withRunFlags(t, "", false)

	var code int
	out := captureStdout(t, func() {
		code = run(runCmdForTest(false), []string{fmt.Sprintf("https://ghe.example.com/owner/repo/actions/runs/%d", snapshotRunID)})
	})

	if code != 0 {
		t.Errorf("exitCode = %d, want 0 (the job passed)", code)
	}
	if !strings.Contains(out, "owner/repo: Add widgets (#42)") {
		t.Errorf("output missing run header, got:\n%s", out)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("output missing job name, got:\n%s", out)
	}
}

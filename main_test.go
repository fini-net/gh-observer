package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
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

package main

import (
	"os"
	"path/filepath"
	"testing"
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

package main

import (
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

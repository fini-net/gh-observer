package github

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
)

var (
	// hostSegment matches a hostname: starts and ends alphanumeric (no bare
	// "." / "-" hosts), dots and hyphens inside. Ports not supported.
	hostSegment      = `([a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?)`
	repoSlugPattern  = regexp.MustCompile(`^([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)$`)
	hostSlugPattern  = regexp.MustCompile(`^` + hostSegment + `/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)$`)
	repoURLPattern   = regexp.MustCompile(`^https?://` + hostSegment + `/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+?)(?:\.git)?/?$`)
	gitSSHRemoteRE   = regexp.MustCompile(`^git@` + hostSegment + `:([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+?)(?:\.git)?/?$`)
	gitHTTPSRemoteRE = regexp.MustCompile(`^https?://` + hostSegment + `/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+?)(?:\.git)?/?$`)
)

// ParseRepoArg extracts host, owner, and repo from a string in "owner/repo",
// "host/owner/repo", or "https://github.com/owner/repo" format. PR URLs and
// Actions run URLs are rejected — this is only for repo-level arguments.
// Any host is accepted (GitHub Enterprise support, issue #479); unknown
// hosts fail later at the auth/API layer.
//
// All-underscore segments (e.g. "_", "__") are rejected even though the
// regex character class allows underscores. This keeps "_" usable as the
// NoOptDefVal sentinel for the --repo flag in main.go: a user typing
// `gh-observer --repo _` gets a clean "invalid repo argument" error rather
// than silently being treated as auto-detect or as a literal owner/repo of
// "_". Embedded underscores (e.g. "my_org/my_repo") are still accepted.
func ParseRepoArg(arg string) (host, owner, repo string, err error) {
	if m := repoSlugPattern.FindStringSubmatch(arg); len(m) == 3 {
		if isAllUnderscoreSegment(m[1]) || isAllUnderscoreSegment(m[2]) {
			return "", "", "", invalidRepoArgError(arg)
		}
		// No host: caller falls back to DefaultHost() (GH_HOST or github.com).
		return "", m[1], m[2], nil
	}
	// "host/owner/repo" — the host must look like a hostname (a dot, to
	// distinguish from a 3-segment owner/repo typo like "org/team/repo"),
	// otherwise it would swallow legitimate slug parse errors.
	if m := hostSlugPattern.FindStringSubmatch(arg); len(m) == 4 && strings.Contains(m[1], ".") {
		if isAllUnderscoreSegment(m[2]) || isAllUnderscoreSegment(m[3]) {
			return "", "", "", invalidRepoArgError(arg)
		}
		return strings.ToLower(m[1]), m[2], m[3], nil
	}
	if m := repoURLPattern.FindStringSubmatch(arg); len(m) == 4 {
		if isAllUnderscoreSegment(m[2]) || isAllUnderscoreSegment(m[3]) {
			return "", "", "", invalidRepoArgError(arg)
		}
		return strings.ToLower(m[1]), m[2], m[3], nil
	}
	return "", "", "", invalidRepoArgError(arg)
}

// invalidRepoArgError is the shared rejection error for ParseRepoArg.
func invalidRepoArgError(arg string) error {
	return fmt.Errorf("invalid repo argument: %q (expected \"owner/repo\", \"host/owner/repo\", or \"https://github.com/owner/repo\")", arg)
}

// isAllUnderscoreSegment returns true for strings composed entirely of
// underscores (e.g. "_", "__", "___"). Such strings are valid against the
// slug regex but are not real GitHub owner/repo names, and "_" is used as
// the --repo auto-detect sentinel in main.go.
func isAllUnderscoreSegment(s string) bool {
	return s != "" && strings.Trim(s, "_") == ""
}

// GetCurrentRepo detects the host, owner, and repo from the current git
// remote. It reads the "origin" remote URL and extracts host/owner/repo from
// either SSH or HTTPS formats. Any host is accepted (GitHub Enterprise
// remotes included); unknown hosts fail later at the auth/API layer.
func GetCurrentRepo() (host, owner, repo string, err error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to detect repo from git remote: %w", err)
	}
	url := strings.TrimSpace(string(out))

	if m := gitSSHRemoteRE.FindStringSubmatch(url); len(m) == 4 {
		debug.Log("detected repo from SSH remote", "host", m[1], "owner", m[2], "repo", m[3])
		return strings.ToLower(m[1]), m[2], m[3], nil
	}
	if m := gitHTTPSRemoteRE.FindStringSubmatch(url); len(m) == 4 {
		debug.Log("detected repo from HTTPS remote", "host", m[1], "owner", m[2], "repo", m[3])
		return strings.ToLower(m[1]), m[2], m[3], nil
	}

	return "", "", "", fmt.Errorf("could not parse owner/repo from git remote URL: %q", url)
}

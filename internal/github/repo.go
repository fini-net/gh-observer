package github

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
)

var (
	repoSlugPattern  = regexp.MustCompile(`^([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+)$`)
	repoURLPattern   = regexp.MustCompile(`^https?://github\.com/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+?)(?:\.git)?/?$`)
	gitSSHRemoteRE   = regexp.MustCompile(`^git@github\.com:([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+?)(?:\.git)?/?$`)
	gitHTTPSRemoteRE = regexp.MustCompile(`^https?://github\.com/([a-zA-Z0-9_.-]+)/([a-zA-Z0-9_.-]+?)(?:\.git)?/?$`)
)

// ParseRepoArg extracts owner and repo from a string in "owner/repo" or
// "https://github.com/owner/repo" format. PR URLs and Actions run URLs are
// rejected — this is only for repo-level arguments.
//
// All-underscore segments (e.g. "_", "__") are rejected even though the
// regex character class allows underscores. This keeps "_" usable as the
// NoOptDefVal sentinel for the --repo flag in main.go: a user typing
// `gh-observer --repo _` gets a clean "invalid repo argument" error rather
// than silently being treated as auto-detect or as a literal owner/repo of
// "_". Embedded underscores (e.g. "my_org/my_repo") are still accepted.
func ParseRepoArg(arg string) (owner, repo string, err error) {
	if m := repoSlugPattern.FindStringSubmatch(arg); len(m) == 3 {
		if isAllUnderscoreSegment(m[1]) || isAllUnderscoreSegment(m[2]) {
			return "", "", fmt.Errorf("invalid repo argument: %q (expected \"owner/repo\" or \"https://github.com/owner/repo\")", arg)
		}
		return m[1], m[2], nil
	}
	if m := repoURLPattern.FindStringSubmatch(arg); len(m) == 3 {
		if isAllUnderscoreSegment(m[1]) || isAllUnderscoreSegment(m[2]) {
			return "", "", fmt.Errorf("invalid repo argument: %q (expected \"owner/repo\" or \"https://github.com/owner/repo\")", arg)
		}
		return m[1], m[2], nil
	}
	return "", "", fmt.Errorf("invalid repo argument: %q (expected \"owner/repo\" or \"https://github.com/owner/repo\")", arg)
}

// isAllUnderscoreSegment returns true for strings composed entirely of
// underscores (e.g. "_", "__", "___"). Such strings are valid against the
// slug regex but are not real GitHub owner/repo names, and "_" is used as
// the --repo auto-detect sentinel in main.go.
func isAllUnderscoreSegment(s string) bool {
	return s != "" && strings.Trim(s, "_") == ""
}

// GetCurrentRepo detects the owner and repo from the current git remote.
// It reads the "origin" remote URL and extracts owner/repo from either SSH
// or HTTPS formats.
func GetCurrentRepo() (owner, repo string, err error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to detect repo from git remote: %w", err)
	}
	url := strings.TrimSpace(string(out))

	if m := gitSSHRemoteRE.FindStringSubmatch(url); len(m) == 3 {
		debug.Log("detected repo from SSH remote", "owner", m[1], "repo", m[2])
		return m[1], m[2], nil
	}
	if m := gitHTTPSRemoteRE.FindStringSubmatch(url); len(m) == 3 {
		debug.Log("detected repo from HTTPS remote", "owner", m[1], "repo", m[2])
		return m[1], m[2], nil
	}

	return "", "", fmt.Errorf("could not parse owner/repo from git remote URL: %q", url)
}

package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/google/go-github/v93/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// DefaultGitHubHost is the public GitHub hostname. Hosts other than this
// are treated as GitHub Enterprise instances and get derived API endpoints.
const DefaultGitHubHost = "github.com"

// defaultHostRegex matches the hostname characters accepted from URLs and
// remotes. Lowercase letters, digits, dots, and hyphens; no ports (a
// documented limitation — see README) and no IPv6 literals.
var defaultHostRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$|^[a-z0-9]$`)

// DefaultHost returns the host used when an argument carries no host of
// its own (bare PR number, owner/repo slug, auto-detect). This mirrors
// the gh CLI: GH_HOST when set to a valid hostname, otherwise github.com.
func DefaultHost() string {
	if h := strings.ToLower(strings.TrimSpace(os.Getenv("GH_HOST"))); h != "" {
		if isValidHost(h) {
			return h
		}
		debug.Log("ignoring invalid GH_HOST value", "value", os.Getenv("GH_HOST"))
	}
	return DefaultGitHubHost
}

// isValidHost reports whether s is a plausible hostname: non-empty,
// matching the host character class, not a slug-looking single segment
// with underscores (those are owner names, not hosts), and not an IP or
// scheme-bearing string.
func isValidHost(s string) bool {
	return defaultHostRegex.MatchString(s)
}

// APIURLEnvOverride names the environment variable that overrides the
// derived enterprise API endpoints: when set, REST uses <url>/ and
// GraphQL uses <url>/graphql for every non-github.com host. This serves
// hosts whose API is not reachable via the api.<host> subdomain
// convention (e.g. a reverse proxy serving both endpoints under one
// base), and gives out-of-package tests a seam to point host-based
// client construction at a local httptest server (issue #530) — the
// host regex rejects ports, so an httptest server is otherwise
// unreachable through host-based dispatch.
//
// The value must be an absolute https URL (or plain http on a loopback
// host): the bearer token rides every request to the override target,
// so plaintext stops at loopback. One URL cannot express GHES's split
// layout (REST /api/v3, GraphQL /api/graphql) — see issue #532.
const APIURLEnvOverride = "GH_OBSERVER_API_URL"

// APIURLsForHost derives the REST and GraphQL API base URLs for a host.
// The public github.com host returns empty strings so callers keep the
// library defaults (api.github.com and githubv4's default endpoint); a
// stray GH_OBSERVER_API_URL never redirects public-host traffic.
// Any other host is treated as a GitHub Enterprise instance using the
// api.<host> subdomain convention: REST at https://api.<host>/ and
// GraphQL at https://api.<host>/graphql — unless GH_OBSERVER_API_URL
// overrides both (see APIURLEnvOverride). An unusable override value —
// unparseable, non-http(s), non-https off loopback — is ignored with a
// debug log, mirroring the GH_HOST validation precedent: clients would
// otherwise fail confusingly at request time.
func APIURLsForHost(host string) (restURL, graphqlURL string) {
	if host == "" || strings.EqualFold(host, DefaultGitHubHost) {
		return "", ""
	}
	if base := strings.TrimSpace(os.Getenv(APIURLEnvOverride)); base != "" {
		if validAPIURL(base) {
			base = strings.TrimSuffix(base, "/")
			return base + "/", base + "/graphql"
		}
		debug.Log("ignoring invalid "+APIURLEnvOverride, "value", base)
	}
	return fmt.Sprintf("https://api.%s/", host), fmt.Sprintf("https://api.%s/graphql", host)
}

// validAPIURL reports whether the GH_OBSERVER_API_URL value is usable: an
// absolute https URL with a host, or a plain http URL on a loopback host.
// The bearer token rides every request to the override target, so
// plaintext http is confined to loopback (local test doubles, a local
// proxy); anything off-loopback must use https.
func validAPIURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return isLoopbackHost(u.Hostname())
	default:
		return false
	}
}

// isLoopbackHost reports whether host is a loopback hostname or literal:
// "localhost", 127.0.0.1, or ::1 (url.URL.Hostname strips ports and IPv6
// brackets). These are the only hosts accepted for plaintext http
// overrides.
func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// GetTokenForHost retrieves an auth token for the given host, mirroring
// the gh CLI's token resolution order:
//
//   - github.com (or GH_HOST when set): GH_TOKEN, then GITHUB_TOKEN, then
//     `gh auth token`
//   - any other host: GH_ENTERPRISE_TOKEN, then GITHUB_ENTERPRISE_TOKEN,
//     then `gh auth token --host <host>`
//
// The env-var split matches go-gh's auth.TokenForHost so users who already
// have enterprise tokens exported for other gh-based tools work unchanged.
func GetTokenForHost(host string) (string, error) {
	if host == "" || strings.EqualFold(host, DefaultGitHubHost) {
		return getTokenForDefaultHost()
	}
	return getTokenForEnterpriseHost(host)
}

// getTokenForDefaultHost resolves the token for github.com (or GH_HOST):
// env vars first, then the gh CLI's default-host token.
func getTokenForDefaultHost() (string, error) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			debug.Log("token from env", "var", name)
			return token, nil
		}
	}
	return tokenFromGhCLI()
}

// getTokenForEnterpriseHost resolves the token for a GitHub Enterprise
// host: enterprise env vars first, then `gh auth token --host <host>`.
func getTokenForEnterpriseHost(host string) (string, error) {
	for _, name := range []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			debug.Log("token from env", "var", name, "host", host)
			return token, nil
		}
	}
	token, err := tokenFromGhCLIForHost(host)
	if err != nil {
		return "", fmt.Errorf("authentication failed for %s: set GH_ENTERPRISE_TOKEN/GITHUB_ENTERPRISE_TOKEN or run `gh auth login --hostname %s`", host, host)
	}
	return token, nil
}

// tokenFromGhCLI shells out to `gh auth token` for the default host token.
func tokenFromGhCLI() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	output, err := cmd.CombinedOutput()
	if err != nil {
		debug.Log("gh auth token failed", "err", err, "output", strings.TrimSpace(string(output)))
		return "", fmt.Errorf("authentication failed: set GITHUB_TOKEN or run `gh auth login`")
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("authentication failed: `gh auth token` returned empty; set GITHUB_TOKEN or run `gh auth login`")
	}
	return token, nil
}

// tokenFromGhCLIForHost shells out to `gh auth token --host <host>` for an
// enterprise host token.
func tokenFromGhCLIForHost(host string) (string, error) {
	cmd := exec.Command("gh", "auth", "token", "--host", host)
	output, err := cmd.CombinedOutput()
	if err != nil {
		debug.Log("gh auth token --host failed", "host", host, "err", err, "output", strings.TrimSpace(string(output)))
		return "", fmt.Errorf("gh auth token --host %s failed", host)
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("`gh auth token --host %s` returned empty", host)
	}
	return token, nil
}

// NewClientFromToken creates a GitHub API client for the given host. An
// empty host or github.com yields the library-default client pointed at
// api.github.com; any other host is treated as GitHub Enterprise and the
// client is configured with derived enterprise base URLs.
func NewClientFromToken(token, host string) (*github.Client, error) {
	restURL, _ := APIURLsForHost(host)
	if restURL == "" {
		return github.NewClient(github.WithAuthToken(token))
	}
	baseURL, err := url.Parse(restURL)
	if err != nil {
		return nil, fmt.Errorf("invalid enterprise API URL for host %s: %w", host, err)
	}
	// Upload URL is unused by gh-observer (it never hits upload
	// endpoints), so the same base URL is passed for both arguments.
	return github.NewClient(
		github.WithAuthToken(token),
		github.WithEnterpriseURLs(baseURL.String(), baseURL.String()),
	)
}

// newGraphQLClient builds a shurcooL/githubv4 client for the given host.
// The default host keeps the library endpoint (api.github.com); an
// enterprise host gets the derived enterprise GraphQL endpoint. The
// httpClient must already carry the auth token.
func newGraphQLClient(host string, httpClient *http.Client) *githubv4.Client {
	_, graphqlURL := APIURLsForHost(host)
	if graphqlURL == "" {
		return githubv4.NewClient(httpClient)
	}
	return githubv4.NewEnterpriseClient(graphqlURL, httpClient)
}

// newAuthenticatedHTTPClient wraps a static token in an oauth2 HTTP client.
func newAuthenticatedHTTPClient(ctx context.Context, token string) *http.Client {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return oauth2.NewClient(ctx, src)
}

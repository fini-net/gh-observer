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
	"github.com/google/go-github/v92/github"
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

// APIURLsForHost derives the REST and GraphQL API base URLs for a host.
// The public github.com host returns empty strings so callers keep the
// library defaults (api.github.com and githubv4's default endpoint).
// Any other host is treated as a GitHub Enterprise instance using the
// api.<host> subdomain convention: REST at https://api.<host>/ and
// GraphQL at https://api.<host>/graphql.
func APIURLsForHost(host string) (restURL, graphqlURL string) {
	if host == "" || strings.EqualFold(host, DefaultGitHubHost) {
		return "", ""
	}
	return fmt.Sprintf("https://api.%s/", host), fmt.Sprintf("https://api.%s/graphql", host)
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

// GetToken retrieves the GitHub token for the default host (github.com or
// GH_HOST). Retained for callers that don't know a host; new code should
// prefer GetTokenForHost.
func GetToken() (string, error) {
	return GetTokenForHost(DefaultHost())
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
	return github.NewClient(
		github.WithAuthToken(token),
		github.WithEnterpriseURLs(baseURL.String(), baseURL.String()),
	)
}

// NewClient creates a GitHub API client for the given host using
// GITHUB_TOKEN/GH_TOKEN env vars or the gh CLI (see GetTokenForHost).
func NewClient(ctx context.Context, host string) (*github.Client, error) {
	token, err := GetTokenForHost(host)
	if err != nil {
		return nil, err
	}
	return NewClientFromToken(token, host)
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

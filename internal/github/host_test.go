package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultHost(t *testing.T) {
	tests := []struct {
		name   string
		ghHost string
		want   string
	}{
		{"unset", "", "github.com"},
		{"valid GH_HOST", "github.example.com", "github.example.com"},
		{"GH_HOST with whitespace", "  github.example.com  ", "github.example.com"},
		{"uppercase GH_HOST is lowercased", "GitHub.Example.Com", "github.example.com"},
		{"invalid GH_HOST ignored (underscore)", "not_a_host", "github.com"},
		{"invalid GH_HOST ignored (spaces)", "github example.com", "github.com"},
		{"invalid GH_HOST ignored (port)", "example.com:8443", "github.com"},
		{"invalid GH_HOST ignored (single dot)", ".", "github.com"},
		{"invalid GH_HOST ignored (leading dot)", ".example.com", "github.com"},
		{"invalid GH_HOST ignored (trailing dot)", "example.com.", "github.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GH_HOST", tt.ghHost)
			if got := DefaultHost(); got != tt.want {
				t.Errorf("DefaultHost() with GH_HOST=%q = %q, want %q", tt.ghHost, got, tt.want)
			}
		})
	}
}

func TestAPIURLsForHost(t *testing.T) {
	tests := []struct {
		host        string
		wantREST    string
		wantGraphQL string
	}{
		{"", "", ""},
		{"github.com", "", ""},
		{"GitHub.com", "", ""},
		// GHES and custom enterprise hosts: path-based endpoints
		// (issue #485, mirroring go-gh: plain GHES serves
		// https://<host>/api/v3 and https://<host>/api/graphql).
		{"github.example.com", "https://github.example.com/api/v3/", "https://github.example.com/api/graphql"},
		{"ghe.corp.local", "https://ghe.corp.local/api/v3/", "https://ghe.corp.local/api/graphql"},
		// GitHub Enterprise Cloud tenants (*.ghe.com): api.<host>
		// subdomain convention.
		{"octocorp.ghe.com", "https://api.octocorp.ghe.com/", "https://api.octocorp.ghe.com/graphql"},
		// The tenant suffix requires a leading label: the apex is not
		// a tenant, so it derives like any other enterprise host.
		{"ghe.com", "https://ghe.com/api/v3/", "https://ghe.com/api/graphql"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			rest, gql := APIURLsForHost(tt.host)
			if rest != tt.wantREST {
				t.Errorf("APIURLsForHost(%q) rest = %q, want %q", tt.host, rest, tt.wantREST)
			}
			if gql != tt.wantGraphQL {
				t.Errorf("APIURLsForHost(%q) graphql = %q, want %q", tt.host, gql, tt.wantGraphQL)
			}
		})
	}
}

func TestAPIURLsForHostOverride(t *testing.T) {
	tests := []struct {
		name        string
		override    string
		wantREST    string
		wantGraphQL string
	}{
		{"https anywhere", "https://ghe.corp.local/api", "https://ghe.corp.local/api/", "https://ghe.corp.local/api/graphql"},
		{"https any host accepted", "https://ghe.example.com", "https://ghe.example.com/", "https://ghe.example.com/graphql"},
		{"http loopback accepted (IPv4)", "http://127.0.0.1:54321", "http://127.0.0.1:54321/", "http://127.0.0.1:54321/graphql"},
		{"http loopback accepted (localhost)", "http://localhost:54321", "http://localhost:54321/", "http://localhost:54321/graphql"},
		{"http loopback accepted (IPv6)", "http://[::1]:54321", "http://[::1]:54321/", "http://[::1]:54321/graphql"},
		{"http off loopback ignored (token safety)", "http://ghe.example.com", "https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
		{"trailing slash trimmed", "http://127.0.0.1:54321/", "http://127.0.0.1:54321/", "http://127.0.0.1:54321/graphql"},
		{"whitespace trimmed", "  http://127.0.0.1:54321  ", "http://127.0.0.1:54321/", "http://127.0.0.1:54321/graphql"},
		{"invalid (no scheme) ignored", "127.0.0.1:54321", "https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
		{"invalid (unsupported scheme) ignored", "ftp://example.com", "https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
		{"invalid (no host) ignored", "http://", "https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
		{"empty ignored", "", "https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(APIURLEnvOverride, tt.override)
			rest, gql := APIURLsForHost("ghe.example.com")
			if rest != tt.wantREST {
				t.Errorf("APIURLsForHost() rest = %q, want %q", rest, tt.wantREST)
			}
			if gql != tt.wantGraphQL {
				t.Errorf("APIURLsForHost() graphql = %q, want %q", gql, tt.wantGraphQL)
			}
		})
	}

	t.Run("override never applies to the public host", func(t *testing.T) {
		t.Setenv(APIURLEnvOverride, "http://127.0.0.1:54321")
		for _, host := range []string{"", "github.com", "GitHub.com"} {
			if rest, gql := APIURLsForHost(host); rest != "" || gql != "" {
				t.Errorf("APIURLsForHost(%q) = %q/%q, want empty strings (public host keeps library defaults)", host, rest, gql)
			}
		}
	})
}

// TestAPIURLsForHostGraphQLOverride pins GH_OBSERVER_GRAPHQL_URL
// (issue #532): it overrides just the GraphQL endpoint for
// non-github.com hosts and wins over the GraphQL URL
// GH_OBSERVER_API_URL derives — which is what makes split API layouts
// (REST and GraphQL under different bases) expressible with the pair.
func TestAPIURLsForHostGraphQLOverride(t *testing.T) {
	tests := []struct {
		name        string
		apiOverride string
		gqlOverride string
		wantREST    string
		wantGraphQL string
	}{
		{"GraphQL only: REST stays derived", "", "https://gql.corp.example.com/api/graphql", "https://ghe.example.com/api/v3/", "https://gql.corp.example.com/api/graphql"},
		{"split layout: API override plus GraphQL override", "https://ghe.example.com", "https://ghe.example.com/api/graphql", "https://ghe.example.com/", "https://ghe.example.com/api/graphql"},
		{"GraphQL override wins over API override's derivation", "https://ghe.example.com", "https://gql.corp.example.com/api/graphql", "https://ghe.example.com/", "https://gql.corp.example.com/api/graphql"},
		{"invalid GraphQL override ignored, API override stands", "https://ghe.example.com", "ftp://example.com", "https://ghe.example.com/", "https://ghe.example.com/graphql"},
		{"off-loopback http GraphQL override ignored (token safety)", "https://ghe.example.com", "http://gql.corp.example.com", "https://ghe.example.com/", "https://ghe.example.com/graphql"},
		{"loopback http GraphQL override accepted", "", "http://127.0.0.1:54321/api/graphql", "https://ghe.example.com/api/v3/", "http://127.0.0.1:54321/api/graphql"},
		{"whitespace trimmed", "", "  https://gql.corp.example.com/api/graphql  ", "https://ghe.example.com/api/v3/", "https://gql.corp.example.com/api/graphql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(APIURLEnvOverride, tt.apiOverride)
			t.Setenv(GraphQLURLEnvOverride, tt.gqlOverride)
			rest, gql := APIURLsForHost("ghe.example.com")
			if rest != tt.wantREST {
				t.Errorf("APIURLsForHost() rest = %q, want %q", rest, tt.wantREST)
			}
			if gql != tt.wantGraphQL {
				t.Errorf("APIURLsForHost() graphql = %q, want %q", gql, tt.wantGraphQL)
			}
		})
	}

	t.Run("override never applies to the public host", func(t *testing.T) {
		t.Setenv(APIURLEnvOverride, "http://127.0.0.1:54321")
		t.Setenv(GraphQLURLEnvOverride, "http://127.0.0.1:54321/api/graphql")
		for _, host := range []string{"", "github.com", "GitHub.com"} {
			if rest, gql := APIURLsForHost(host); rest != "" || gql != "" {
				t.Errorf("APIURLsForHost(%q) = %q/%q, want empty strings (public host keeps library defaults)", host, rest, gql)
			}
		}
	})
}

// TestFetchCheckRunsGraphQLOverrideEndpoint pins the GraphQL endpoint the
// override produces (review of #531): with GH_OBSERVER_API_URL set, the
// exported FetchCheckRunsGraphQL must send its query as POST <url>/graphql
// exactly — the contract the README documents. Together with the "override
// redirects enterprise client" case in TestNewClientFromTokenHostRouting
// (REST base <url>/api/v3/), this pins the endpoint pair a configured
// override yields. It also exercises the real client construction
// (newGraphQLClient, newAuthenticatedHTTPClient) that in-package mock
// tests bypass.
func TestFetchCheckRunsGraphQLOverrideEndpoint(t *testing.T) {
	requests := make(chan [2]string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- [2]string{r.Method, r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"repository": {"pullRequest": {"commits": {"nodes": []}}},
				"rateLimit": {"limit": 5000, "remaining": 4999}
			}
		}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv(APIURLEnvOverride, server.URL)

	checkRuns, anchor, rateLimit, err := FetchCheckRunsGraphQL(context.Background(), "test-token", "ghe.example.com", "owner", "repo", 1)
	if err != nil {
		t.Fatalf("FetchCheckRunsGraphQL() error: %v", err)
	}
	if len(checkRuns) != 0 || !anchor.IsZero() {
		t.Errorf("empty-commits response should parse to zero runs and a zero anchor, got %d runs, anchor %v", len(checkRuns), anchor)
	}
	if rateLimit != 4999 {
		t.Errorf("rateLimit = %d, want 4999", rateLimit)
	}
	select {
	case req := <-requests:
		if req[0] != http.MethodPost || req[1] != "/graphql" {
			t.Errorf("GraphQL request = %s %s, want POST /graphql", req[0], req[1])
		}
	default:
		t.Error("expected exactly one GraphQL request, got none")
	}
}

func TestGetTokenForHostEnvPrecedence(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // gh CLI absent: env vars only

	t.Run("default host prefers GH_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "gh-token")
		t.Setenv("GITHUB_TOKEN", "github-token")
		token, err := GetTokenForHost("github.com")
		if err != nil {
			t.Fatalf("GetTokenForHost() error: %v", err)
		}
		if token != "gh-token" {
			t.Errorf("token = %q, want gh-token (GH_TOKEN wins)", token)
		}
	})

	t.Run("default host falls back to GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "github-token")
		token, err := GetTokenForHost("github.com")
		if err != nil {
			t.Fatalf("GetTokenForHost() error: %v", err)
		}
		if token != "github-token" {
			t.Errorf("token = %q, want github-token", token)
		}
	})

	t.Run("enterprise host prefers GH_ENTERPRISE_TOKEN", func(t *testing.T) {
		t.Setenv("GH_ENTERPRISE_TOKEN", "ent-token")
		t.Setenv("GITHUB_ENTERPRISE_TOKEN", "github-ent-token")
		token, err := GetTokenForHost("github.example.com")
		if err != nil {
			t.Fatalf("GetTokenForHost() error: %v", err)
		}
		if token != "ent-token" {
			t.Errorf("token = %q, want ent-token (GH_ENTERPRISE_TOKEN wins)", token)
		}
	})

	t.Run("enterprise host falls back to GITHUB_ENTERPRISE_TOKEN", func(t *testing.T) {
		t.Setenv("GH_ENTERPRISE_TOKEN", "")
		t.Setenv("GITHUB_ENTERPRISE_TOKEN", "github-ent-token")
		token, err := GetTokenForHost("github.example.com")
		if err != nil {
			t.Fatalf("GetTokenForHost() error: %v", err)
		}
		if token != "github-ent-token" {
			t.Errorf("token = %q, want github-ent-token", token)
		}
	})

	t.Run("enterprise host never reads GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GH_ENTERPRISE_TOKEN", "")
		t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "should-not-leak")
		_, err := GetTokenForHost("github.example.com")
		if err == nil {
			t.Fatal("GetTokenForHost(enterprise) should fail when only GITHUB_TOKEN is set")
		}
		if !strings.Contains(err.Error(), "gh auth login --hostname github.example.com") {
			t.Errorf("error = %v, want hostname hint", err)
		}
	})

	t.Run("default host never reads GH_ENTERPRISE_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_ENTERPRISE_TOKEN", "should-not-leak")
		t.Setenv("GITHUB_ENTERPRISE_TOKEN", "should-not-leak")
		_, err := GetTokenForHost("github.com")
		if err == nil {
			t.Fatal("GetTokenForHost(github.com) should fail when only enterprise tokens are set")
		}
	})

	t.Run("empty host treated as default host", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "gh-token")
		token, err := GetTokenForHost("")
		if err != nil {
			t.Fatalf("GetTokenForHost() error: %v", err)
		}
		if token != "gh-token" {
			t.Errorf("token = %q, want gh-token", token)
		}
	})
}

func TestGetTokenForHostGhCLIDefaultHost(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	// gh CLI present on the dev machine: default-host token must resolve
	// through `gh auth token`. If gh is absent or unauthenticated the test
	// environment would fail; skip instead of flaking.
	token, err := GetTokenForHost("github.com")
	if err != nil {
		t.Skipf("gh CLI unavailable or unauthenticated: %v", err)
	}
	if token == "" {
		t.Fatal("token must be non-empty on success")
	}
}

func TestNewClientFromTokenHostRouting(t *testing.T) {
	t.Run("default host targets api.github.com", func(t *testing.T) {
		client, err := NewClientFromToken("tok", "github.com")
		if err != nil {
			t.Fatalf("NewClientFromToken() error: %v", err)
		}
		if got := client.BaseURL(); got != "https://api.github.com/" {
			t.Errorf("BaseURL = %q, want https://api.github.com/", got)
		}
	})

	t.Run("empty host targets api.github.com", func(t *testing.T) {
		client, err := NewClientFromToken("tok", "")
		if err != nil {
			t.Fatalf("NewClientFromToken() error: %v", err)
		}
		if got := client.BaseURL(); got != "https://api.github.com/" {
			t.Errorf("BaseURL = %q, want https://api.github.com/", got)
		}
	})

	t.Run("GHES host targets <host>/api/v3/", func(t *testing.T) {
		client, err := NewClientFromToken("tok", "github.example.com")
		if err != nil {
			t.Fatalf("NewClientFromToken() error: %v", err)
		}
		// WithEnterpriseURLs leaves a base ending in /api/v3/ untouched,
		// so the derived REST URL is the BaseURL verbatim.
		if got := client.BaseURL(); got != "https://github.example.com/api/v3/" {
			t.Errorf("BaseURL = %q, want https://github.example.com/api/v3/", got)
		}
	})

	t.Run("GHEC tenant targets api.<host>", func(t *testing.T) {
		client, err := NewClientFromToken("tok", "octocorp.ghe.com")
		if err != nil {
			t.Fatalf("NewClientFromToken() error: %v", err)
		}
		// The api. host prefix suppresses WithEnterpriseURLs' api/v3/
		// completion, so the subdomain base is used verbatim.
		if got := client.BaseURL(); got != "https://api.octocorp.ghe.com/" {
			t.Errorf("BaseURL = %q, want https://api.octocorp.ghe.com/", got)
		}
	})

	t.Run("override redirects enterprise client", func(t *testing.T) {
		t.Setenv(APIURLEnvOverride, "http://127.0.0.1:54321")
		client, err := NewClientFromToken("tok", "ghe.example.com")
		if err != nil {
			t.Fatalf("NewClientFromToken() error: %v", err)
		}
		// go-github appends api/v3/ to hosts not under api.* — the test
		// server routes on the api/v3 prefix. What matters here is that
		// the base points at the override, not api.ghe.example.com.
		if got := client.BaseURL(); got != "http://127.0.0.1:54321/api/v3/" {
			t.Errorf("BaseURL = %q, want http://127.0.0.1:54321/api/v3/", got)
		}
	})
}

// TestNewClientFromTokenCredentialOriginRestriction pins go-github v93's
// credential-origin contract (go-github #4564, the v92→v93 breaking
// change): the bearer token rides only requests aimed at the client's
// configured API origin, and a redirect hop to any other origin goes out
// unauthenticated. v92 attached the token below the http.Client redirect
// layer, so it followed cross-origin hops — the class of leak the change
// closes. gh-observer builds every enterprise client through
// NewClientFromToken (WithAuthToken plus WithEnterpriseURLs, so the
// configured origin is the derived or overridden API base), which makes
// this the path that must keep holding across future major bumps.
func TestNewClientFromTokenCredentialOriginRestriction(t *testing.T) {
	authorizationA := make(chan string, 1)
	authorizationB := make(chan string, 1)

	// Server B is the redirect target: its own port makes it a different
	// origin than the configured API base, per go-github's sameOrigin
	// (scheme, host, and port must all match).
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case authorizationB <- r.Header.Get("Authorization"):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login": "octocat"}`))
	}))
	t.Cleanup(serverB.Close)

	// Server A is the configured origin. Every request it receives is
	// redirected to server B, so one API call exercises both hops.
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case authorizationA <- r.Header.Get("Authorization"):
		default:
		}
		http.Redirect(w, r, serverB.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(serverA.Close)

	t.Setenv(APIURLEnvOverride, serverA.URL)

	client, err := NewClientFromToken("tok", "ghe.example.com")
	if err != nil {
		t.Fatalf("NewClientFromToken() error: %v", err)
	}
	user, _, err := client.Users.Get(context.Background(), "octocat")
	if err != nil {
		t.Fatalf("Users.Get() error: %v", err)
	}
	if user.GetLogin() != "octocat" {
		t.Errorf("login = %q, want octocat", user.GetLogin())
	}

	// The configured origin received the bearer token...
	select {
	case auth := <-authorizationA:
		if auth != "Bearer tok" {
			t.Errorf("Authorization at configured origin = %q, want %q", auth, "Bearer tok")
		}
	default:
		t.Error("no request reached the configured origin")
	}
	// ...and the redirect hop did not.
	select {
	case auth := <-authorizationB:
		if auth != "" {
			t.Errorf("Authorization on redirect hop = %q, want empty (credentials must not follow a redirect off the configured origin)", auth)
		}
	default:
		t.Error("no redirect hop reached the redirect target")
	}
}

// TestSplitLayoutOverrides pins the GH_OBSERVER_API_URL +
// GH_OBSERVER_GRAPHQL_URL pair end to end (issue #532): split API
// layouts — REST and GraphQL under different bases — must be
// expressible, and each override must send only its own protocol to
// its own target. With the pair pointing at two distinct local
// servers, the REST request lands under /api/v3/ on the API-override
// server while the GraphQL query POSTs to /api/graphql on the
// GraphQL-override server. This exercises the real client construction
// both overrides flow through (NewClientFromToken, newGraphQLClient,
// newAuthenticatedHTTPClient), the paths in-package mock tests bypass.
func TestSplitLayoutOverrides(t *testing.T) {
	restPaths := make(chan string, 2)
	restServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case restPaths <- r.URL.Path:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number": 42, "state": "open", "head": {"sha": "abc123def456"}}`))
	}))
	t.Cleanup(restServer.Close)

	graphqlPaths := make(chan string, 2)
	graphqlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case graphqlPaths <- r.URL.Path:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"repository": {"pullRequest": {"commits": {"nodes": []}}},
				"rateLimit": {"limit": 5000, "remaining": 4999}
			}
		}`))
	}))
	t.Cleanup(graphqlServer.Close)

	t.Setenv(APIURLEnvOverride, restServer.URL)
	t.Setenv(GraphQLURLEnvOverride, graphqlServer.URL+"/api/graphql")

	// REST: the pull-request fetch lands under /api/v3/ (go-github's
	// WithEnterpriseURLs completes the override base) on the REST server.
	client, err := NewClientFromToken("test-token", "ghe.example.com")
	if err != nil {
		t.Fatalf("NewClientFromToken() error: %v", err)
	}
	if _, _, err := client.PullRequests.Get(context.Background(), "owner", "repo", 42); err != nil {
		t.Fatalf("PullRequests.Get() error: %v", err)
	}
	select {
	case path := <-restPaths:
		if !strings.HasPrefix(path, "/api/v3/") {
			t.Errorf("REST request path = %q, want /api/v3/ prefix", path)
		}
	default:
		t.Error("expected a REST request on the API-override server, got none")
	}

	// GraphQL: the check-runs query POSTs to /api/graphql exactly on the
	// GraphQL server, not to the API override.
	_, _, rateLimit, err := FetchCheckRunsGraphQL(context.Background(), "test-token", "ghe.example.com", "owner", "repo", 42)
	if err != nil {
		t.Fatalf("FetchCheckRunsGraphQL() error: %v", err)
	}
	if rateLimit != 4999 {
		t.Errorf("rateLimit = %d, want 4999", rateLimit)
	}
	select {
	case path := <-graphqlPaths:
		if path != "/api/graphql" {
			t.Errorf("GraphQL request path = %q, want /api/graphql", path)
		}
	default:
		t.Error("expected a GraphQL request on the GraphQL-override server, got none")
	}
	select {
	case path := <-restPaths:
		t.Errorf("unexpected REST-side request %q during the GraphQL fetch", path)
	default:
	}
}

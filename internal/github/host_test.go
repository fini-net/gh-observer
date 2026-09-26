package github

import (
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
		{"github.example.com", "https://api.github.example.com/", "https://api.github.example.com/graphql"},
		{"ghe.corp.local", "https://api.ghe.corp.local/", "https://api.ghe.corp.local/graphql"},
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

	t.Run("enterprise host targets api.<host>", func(t *testing.T) {
		client, err := NewClientFromToken("tok", "github.example.com")
		if err != nil {
			t.Fatalf("NewClientFromToken() error: %v", err)
		}
		if got := client.BaseURL(); got != "https://api.github.example.com/" {
			t.Errorf("BaseURL = %q, want https://api.github.example.com/", got)
		}
	})
}

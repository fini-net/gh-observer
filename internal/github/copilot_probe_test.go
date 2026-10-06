package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v92/github"
)

// TestCopilotReviewerExistsOnClient covers the host capability probe
// (issue #442): a 404 for the Copilot reviewer user means Copilot code
// review cannot exist on the host (e.g. GitHub Enterprise Server without
// Copilot), while a 200 — or any non-404 error — means keep Copilot
// detection enabled.
func TestCopilotReviewerExistsOnClient(t *testing.T) {
	t.Run("404 means incapable host", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		client := newTestClient(t, handler)
		if got := copilotReviewerExistsOnClient(context.Background(), client); got {
			t.Error("expected false for 404 (host without the Copilot reviewer app)")
		}
	})

	t.Run("200 means capable host", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"login":"copilot-pull-request-reviewer[bot]","type":"Bot"}`))
		})
		client := newTestClient(t, handler)
		if got := copilotReviewerExistsOnClient(context.Background(), client); !got {
			t.Error("expected true for 200 (reviewer app exists)")
		}
	})

	t.Run("5xx conservatively means capable", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		client := newTestClient(t, handler)
		// A flaky host must not silently disable Copilot detection — the
		// per-PR scan stays the source of truth.
		if got := copilotReviewerExistsOnClient(context.Background(), client); !got {
			t.Error("expected true for 5xx (conservative default; never disable on transient errors)")
		}
	})

	t.Run("401 conservatively means capable", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
		client := newTestClient(t, handler)
		if got := copilotReviewerExistsOnClient(context.Background(), client); !got {
			t.Error("expected true for 401 (auth errors are not absence)")
		}
	})

	t.Run("probes the [bot]-suffixed login", func(t *testing.T) {
		var gotPath string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		})
		client := newTestClient(t, handler)
		_ = copilotReviewerExistsOnClient(context.Background(), client)
		// go-github passes the login through unescaped, so the raw [bot]
		// suffix arrives intact in r.URL.Path (verified: github.com serves
		// both this and the %5Bbot%5D form).
		if !strings.Contains(gotPath, "/users/copilot-pull-request-reviewer[bot]") {
			t.Errorf("probe path = %q, want /users/copilot-pull-request-reviewer[bot]", gotPath)
		}
	})

	t.Run("probe timeout conservatively means capable", func(t *testing.T) {
		// A hung/slow host must not disable detection: the timeout leaves
		// the per-PR scan as the source of truth.
		origTimeout := copilotProbeTimeout
		copilotProbeTimeout = 50 * time.Millisecond
		t.Cleanup(func() { copilotProbeTimeout = origTimeout })

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(1 * time.Second) // far beyond the shrunken timeout
			w.WriteHeader(http.StatusOK)
		})
		client := newTestClient(t, handler)
		started := time.Now()
		if got := copilotReviewerExistsOnClient(context.Background(), client); !got {
			t.Error("expected true on probe timeout (conservative default; never disable on a hung host)")
		}
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Errorf("probe should respect copilotProbeTimeout, took %v", elapsed)
		}
	})
}

// TestCopilotReviewerExistsOnHost covers the host-level dispatch: the public
// host short-circuits to capable without any request (the app provably
// exists there — verified 2026-10-06 that GET
// /users/copilot-pull-request-reviewer[bot] returns 200 on github.com, both
// unauthenticated and tokened), while enterprise hosts get the real probe.
// A github.com 404 is impossible to hit by construction, which is the point:
// a false "incapable" can only ever originate from an enterprise host where
// 404 is definitive.
func TestCopilotReviewerExistsOnHost(t *testing.T) {
	t.Run("github.com short-circuits to capable without a request", func(t *testing.T) {
		// The short-circuit must hold for the empty host and
		// case-insensitively, mirroring APIURLsForHost's host matching.
		// The cancelled context makes any accidental real request fail
		// immediately (which would still conservatively report true), so
		// what this really locks in is the contract that matters
		// behaviorally: the public host ALWAYS reports capable, never
		// touches the network in a way that could disable detection, and
		// returns promptly.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for _, host := range []string{"", "github.com", "GITHUB.COM"} {
			started := time.Now()
			if got := CopilotReviewerExistsOnHost(ctx, "ghp_not-a-real-token", host); !got {
				t.Errorf("CopilotReviewerExistsOnHost(%q) = false, want true (public host is always capable)", host)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Errorf("CopilotReviewerExistsOnHost(%q) took %v; expected an immediate short-circuit", host, elapsed)
			}
		}
	})

	t.Run("unparseable enterprise host conservatively means capable", func(t *testing.T) {
		// NewClientFromToken fails on an invalid derived API URL; the
		// probe must keep detection enabled rather than silently
		// disabling the feature (per-PR error paths surface the failure).
		if got := CopilotReviewerExistsOnHost(context.Background(), "token", "ghe .example.com"); !got {
			t.Error("expected true when client construction fails (conservative default)")
		}
	})

	t.Run("enterprise host 404 means incapable", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		// Route an "enterprise" host at the test server by pointing the
		// client at it directly — this exercises the same
		// copilotReviewerExistsOnClient 404 logic an enterprise host
		// reaches after client construction succeeds.
		client, err := github.NewClient(github.WithURLs(ptrTo(server.URL+"/"), ptrTo(server.URL+"/")))
		if err != nil {
			t.Fatalf("client: %v", err)
		}
		if got := copilotReviewerExistsOnClient(context.Background(), client); got {
			t.Error("expected false for 404 on an enterprise host without the Copilot reviewer app")
		}
	})
}

// TestNormalizeRateLimit locks in the enterprise rate-limit semantics
// (issue #442): GitHub Enterprise Server with rate limiting disabled
// provides no rate-limit data (GraphQL rateLimit: null, REST without
// X-RateLimit-* headers), which must normalize to the unknown default
// instead of a spurious "0 remaining" that pins the app in permanent
// backoff. The limit field is the discriminator: absent data has limit 0;
// a real quota always carries a positive limit, and a real 0 remaining
// (the request that consumed the last quota point) must be preserved so
// backoff engages for the requests that follow.
func TestNormalizeRateLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		remaining int
		want      int
	}{
		{"null rateLimit (GHES, rate limiting disabled)", 0, 0, UnknownRateLimit},
		{"REST without X-RateLimit headers (limit 0)", 0, 0, UnknownRateLimit},
		{"absent data with stray remaining", 0, 42, UnknownRateLimit},
		{"real quota with remaining 0 is preserved (last-point request)", 5000, 0, 0},
		{"real quota near-exhaustion is preserved", 5000, 5, 5},
		{"normal observation passes through", 5000, 4321, 4321},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRateLimit(tt.limit, tt.remaining); got != tt.want {
				t.Errorf("normalizeRateLimit(%d, %d) = %d, want %d", tt.limit, tt.remaining, got, tt.want)
			}
		})
	}
}

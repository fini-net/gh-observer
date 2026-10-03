package github

import (
	"context"
	"net/http"
	"strings"
	"testing"
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

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
		if !strings.Contains(gotPath, "/users/copilot-pull-request-reviewer") {
			t.Errorf("probe path = %q, want it to target the copilot-pull-request-reviewer user", gotPath)
		}
		if !strings.Contains(gotPath, "[bot]") {
			t.Errorf("probe path = %q, want the [bot]-suffixed REST login", gotPath)
		}
	})
}

// TestNormalizeRateLimit locks in the enterprise rate-limit semantics
// (issue #442): GitHub Enterprise Server with rate limiting disabled
// returns rateLimit: null (zero-value Rate), which must normalize to the
// unknown default instead of a spurious "0 remaining" that pins the app in
// permanent backoff.
func TestNormalizeRateLimit(t *testing.T) {
	tests := []struct {
		name           string
		limit          int
		remaining      int
		want           int
		wantUnknownMsg string
	}{
		{"null rateLimit (GHES, rate limiting disabled)", 0, 0, unknownRateLimit, ""},
		{"real quota with remaining 0 on a successful query is implausible", 5000, 0, unknownRateLimit, ""},
		{"normal observation passes through", 5000, 4321, 4321, ""},
		{"low observation passes through", 5000, 5, 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRateLimit(tt.limit, tt.remaining); got != tt.want {
				t.Errorf("normalizeRateLimit(%d, %d) = %d, want %d", tt.limit, tt.remaining, got, tt.want)
			}
		})
	}
}

package github

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/go-github/v92/github"
)

// copilotReviewerBotLogin is the REST form of the Copilot code review
// GitHub App login. The GraphQL Bot login in reviews.go is
// "copilot-pull-request-reviewer" (no suffix); REST user lookups address
// app actors with the "[bot]" suffix.
const copilotReviewerBotLogin = "copilot-pull-request-reviewer[bot]"

// copilotReviewerUnavailable reports whether err is a 404 from the
// Copilot reviewer user lookup. Anything else (5xx, network, auth) is NOT
// treated as absence: the probe must only disable Copilot detection on a
// definitive "this user does not exist on this host" answer, never on a
// flaky or mis-permissioned network. Note some enterprise setups can 404
// for SSO/token-scope reasons; if that turns out to bite, the per-PR scan
// (two-consecutive-not-requested) is the fallback that self-resolves.
func copilotReviewerUnavailable(err error) bool {
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) {
		return false
	}
	return ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound
}

// copilotReviewerExistsOnClient reports whether the Copilot code review
// GitHub App exists, using an already-configured client. GitHub Enterprise
// Server instances that lack Copilot code review have no
// copilot-pull-request-reviewer[bot] user at all, so a 404 definitively
// distinguishes "Copilot cannot exist here" from "Copilot not enabled for
// this repo" (which the per-PR review-request scan already handles via the
// two-consecutive-not-requested rule, self-resolving in ~25s).
//
// Any other error (5xx, network, auth) conservatively reports true —
// gh-observer keeps polling per-PR rather than silently disabling a
// feature the user may rely on.
func copilotReviewerExistsOnClient(ctx context.Context, client *github.Client) bool {
	// Short timeout: this is a startup capability probe, not a data path.
	// A slow host must not hold up the Copilot gate arming; timing out
	// leaves detection enabled (conservative default).
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _, err := client.Users.Get(ctx, copilotReviewerBotLogin)
	if copilotReviewerUnavailable(err) {
		return false
	}
	// Present, or a transient/unknown error: assume present; per-PR
	// detection remains the source of truth.
	return true
}

// CopilotReviewerExistsOnHost reports whether the Copilot code review
// GitHub App exists on host, using a client built for that host.
// See copilotReviewerExistsOnClient for the detection semantics.
func CopilotReviewerExistsOnHost(ctx context.Context, token, host string) bool {
	client, err := NewClientFromToken(token, host)
	if err != nil {
		// Client construction failed (bad enterprise URL); keep Copilot
		// detection enabled and let the existing per-PR error paths
		// surface the failure.
		return true
	}
	return copilotReviewerExistsOnClient(ctx, client)
}

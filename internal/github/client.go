package github

import (
	"fmt"
	"math"

	"github.com/shurcooL/githubv4"
)

// safeGraphQLInt converts an architecture-dependent int to githubv4.Int
// (backed by int32) with a bounds check, preventing silent truncation on
// platforms where int is 64-bit. Returns an error if the value is out of
// int32 range. Resolves CodeQL "Incorrect conversion between integer types"
// finding on githubv4.Int(prNumber) conversions.
func safeGraphQLInt(n int) (githubv4.Int, error) {
	if n > math.MaxInt32 || n < math.MinInt32 {
		return 0, fmt.Errorf("value %d exceeds int32 range for GraphQL Int scalar", n)
	}
	return githubv4.Int(n), nil
}

// UnknownRateLimit is the value used when the API reports no usable
// rate-limit information (absent rateLimit object or headers). It mirrors
// github.com's default hourly quota (5000) so thresholds (backoff at <10,
// history fetch at >=100) treat "unknown" the same as "plenty available".
const UnknownRateLimit = 5000

// normalizeRateLimit interprets rate-limit data from a request that
// succeeded. GitHub Enterprise Server instances with rate limiting disabled
// (the default) provide no rate-limit data: GraphQL returns rateLimit: null
// (a zero-value struct — Limit 0, Remaining 0) and REST omits the
// X-RateLimit-* headers (zero-value Rate). Reading that as "0 remaining"
// pinned the app in permanent rate-limit backoff while every request
// succeeded (issue #442: a Copilot review row spun "in progress" for 192h
// on an enterprise host whose queries all succeeded).
//
// The discriminator is the limit field: a real quota always carries a
// positive limit (github.com: 5000), so limit <= 0 means the data is
// absent → unknown. A positive limit with remaining 0 is preserved as-is:
// the request that consumes the last point of quota succeeds with
// remaining 0, and backoff must engage for the requests that follow.
func normalizeRateLimit(limit, remaining int) int {
	if limit <= 0 {
		return UnknownRateLimit
	}
	return remaining
}

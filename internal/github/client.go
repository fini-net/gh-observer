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
// rate-limit information. It mirrors github.com's default hourly quota
// (5000) so thresholds (backoff at <10, history fetch at >=100) treat
// "unknown" the same as "plenty available".
const UnknownRateLimit = 5000

// unknownRateLimit is retained as an unexported alias for internal call
// sites; new code should use UnknownRateLimit.
const unknownRateLimit = UnknownRateLimit

// normalizeRateLimit interprets a GraphQL rateLimit object from a query that
// succeeded. GitHub Enterprise Server instances with rate limiting disabled
// (the default) return rateLimit: null, which unmarshals as a zero-value
// struct — Limit 0 and Remaining 0. Treating that as a real "0 remaining"
// would pin the app in permanent rate-limit backoff while every request
// still succeeds (issue #442 comment: a Copilot review row spun "in
// progress" for 192h on an enterprise host whose queries all succeeded).
//
// A genuinely exhausted github.com quota cannot look like this: exhaustion
// fails the request itself (403), so a successful response with Remaining 0
// is not a real quota reading. Callers therefore normalize:
//   - null/absent rateLimit (Limit == 0): unknown -> unknownRateLimit
//   - real quota with Remaining 0: also implausible on success -> unknownRateLimit
//   - anything else: the observed Remaining
//
// limit is the rateLimit object's limit field (the hourly quota); remaining
// is its remaining field. REST callers pass resp.Rate.Limit/Remaining the
// same way, where an enterprise instance with rate limiting disabled leaves
// the X-RateLimit-* headers at their Go zero values.
func normalizeRateLimit(limit, remaining int) int {
	if limit <= 0 || remaining <= 0 {
		return unknownRateLimit
	}
	return remaining
}

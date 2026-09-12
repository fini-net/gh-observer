package github

import (
	"fmt"
	"time"
)

const TimestampFormat = "2006-01-02T15:04:05Z"

// ParseTimestamp parses a REST API timestamp in the exact TimestampFormat
// shape. A successful parse is guaranteed to round-trip: formatting the
// result with TimestampFormat reproduces the input byte-for-byte. The
// explicit check is needed because time.Parse silently accepts (and drops)
// fractional seconds that the layout does not describe, e.g.
// "2024-06-01T12:34:56.5Z" — found by FuzzParseTimestamp.
func ParseTimestamp(s string) (time.Time, error) {
	parsed, err := time.Parse(TimestampFormat, s)
	if err != nil {
		return time.Time{}, err
	}
	if parsed.Format(TimestampFormat) != s {
		return time.Time{}, fmt.Errorf("timestamp %q does not match format %q exactly", s, TimestampFormat)
	}
	return parsed, nil
}

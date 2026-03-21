package tui

import "time"

const (
	slowJobThreshold     = 2 * time.Minute
	verySlowJobThreshold = 3 * time.Minute
	slowLogRuntimeMin    = time.Minute
	slowLogFetchInterval = 10 * time.Second

	rateBackoffThreshold = 10
	minRateLimitForFetch = 100
	fiveLogsFromEndOfJob = 5
)

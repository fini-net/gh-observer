package tui

import "time"

const (
	slowJobThreshold     = 2 * time.Minute
	verySlowJobThreshold = 3 * time.Minute

	rateBackoffThreshold = 10
	minRateLimitForFetch = 100

	historyFetchDelay = 10 * time.Second

	slowLogThreshold = 1 * time.Minute // Show live logs for jobs running longer than this
	slowLogLineCount = 5               // Number of log lines to display per slow job
)

package tui

import "time"

const (
	slowJobThreshold     = 2 * time.Minute
	verySlowJobThreshold = 3 * time.Minute

	rateBackoffThreshold = 10
	rateWarningThreshold = 500
	minRateLimitForFetch = 100

	historyFetchDelay = 10 * time.Second

	minCheckAppearanceRatio = 0.3
	startupGracePeriod      = 2 * time.Minute
)

package github

import "time"

const TimestampFormat = "2006-01-02T15:04:05Z"

func ParseTimestamp(s string) (time.Time, error) {
	return time.Parse(TimestampFormat, s)
}

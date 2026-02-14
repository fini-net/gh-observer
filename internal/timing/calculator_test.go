package timing

import (
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

func TestQueueLatency(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		commitTime  time.Time
		check       ghclient.CheckRunInfo
		wantSeconds int
		wantZero    bool
	}{
		{
			name:       "nil StartedAt returns zero",
			commitTime: now,
			check: ghclient.CheckRunInfo{
				StartedAt: nil,
			},
			wantZero: true,
		},
		{
			name:       "zero commitTime returns zero",
			commitTime: time.Time{},
			check: ghclient.CheckRunInfo{
				StartedAt: ptrTime(now.Add(30 * time.Second)),
			},
			wantZero: true,
		},
		{
			name:       "normal latency calculation",
			commitTime: now,
			check: ghclient.CheckRunInfo{
				StartedAt: ptrTime(now.Add(45 * time.Second)),
			},
			wantSeconds: 45,
		},
		{
			name:       "zero latency when check starts immediately",
			commitTime: now,
			check: ghclient.CheckRunInfo{
				StartedAt: ptrTime(now),
			},
			wantSeconds: 0,
		},
		{
			name:       "long latency",
			commitTime: now,
			check: ghclient.CheckRunInfo{
				StartedAt: ptrTime(now.Add(5 * time.Minute)),
			},
			wantSeconds: 300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QueueLatency(tt.commitTime, tt.check)
			if tt.wantZero {
				if got != 0 {
					t.Errorf("QueueLatency() = %v, want 0", got)
				}
				return
			}
			gotSeconds := int(got.Seconds())
			if gotSeconds != tt.wantSeconds {
				t.Errorf("QueueLatency() = %v seconds, want %v seconds", gotSeconds, tt.wantSeconds)
			}
		})
	}
}

func TestRuntime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		check    ghclient.CheckRunInfo
		wantZero bool
	}{
		{
			name: "non in_progress status returns zero",
			check: ghclient.CheckRunInfo{
				Status:    "completed",
				StartedAt: ptrTime(now.Add(-1 * time.Minute)),
			},
			wantZero: true,
		},
		{
			name: "nil StartedAt returns zero",
			check: ghclient.CheckRunInfo{
				Status:    "in_progress",
				StartedAt: nil,
			},
			wantZero: true,
		},
		{
			name: "queued status returns zero",
			check: ghclient.CheckRunInfo{
				Status:    "queued",
				StartedAt: ptrTime(now),
			},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Runtime(tt.check)
			if tt.wantZero {
				if got != 0 {
					t.Errorf("Runtime() = %v, want 0", got)
				}
				return
			}
		})
	}
}

func TestFinalDuration(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		check       ghclient.CheckRunInfo
		wantSeconds int
		wantZero    bool
	}{
		{
			name: "nil StartedAt returns zero",
			check: ghclient.CheckRunInfo{
				StartedAt:   nil,
				CompletedAt: ptrTime(now),
			},
			wantZero: true,
		},
		{
			name: "nil CompletedAt returns zero",
			check: ghclient.CheckRunInfo{
				StartedAt:   ptrTime(now),
				CompletedAt: nil,
			},
			wantZero: true,
		},
		{
			name: "both nil returns zero",
			check: ghclient.CheckRunInfo{
				StartedAt:   nil,
				CompletedAt: nil,
			},
			wantZero: true,
		},
		{
			name: "normal duration calculation",
			check: ghclient.CheckRunInfo{
				StartedAt:   ptrTime(now),
				CompletedAt: ptrTime(now.Add(2 * time.Minute)),
			},
			wantSeconds: 120,
		},
		{
			name: "short duration",
			check: ghclient.CheckRunInfo{
				StartedAt:   ptrTime(now),
				CompletedAt: ptrTime(now.Add(5 * time.Second)),
			},
			wantSeconds: 5,
		},
		{
			name: "long duration over an hour",
			check: ghclient.CheckRunInfo{
				StartedAt:   ptrTime(now),
				CompletedAt: ptrTime(now.Add(90 * time.Minute)),
			},
			wantSeconds: 5400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FinalDuration(tt.check)
			if tt.wantZero {
				if got != 0 {
					t.Errorf("FinalDuration() = %v, want 0", got)
				}
				return
			}
			gotSeconds := int(got.Seconds())
			if gotSeconds != tt.wantSeconds {
				t.Errorf("FinalDuration() = %v seconds, want %v seconds", gotSeconds, tt.wantSeconds)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "zero duration",
			duration: 0,
			want:     "0s",
		},
		{
			name:     "negative duration",
			duration: -5 * time.Second,
			want:     "0s",
		},
		{
			name:     "seconds only",
			duration: 5 * time.Second,
			want:     "5s",
		},
		{
			name:     "seconds rounded down",
			duration: 5*time.Second + 400*time.Millisecond,
			want:     "5s",
		},
		{
			name:     "seconds rounded up",
			duration: 5*time.Second + 600*time.Millisecond,
			want:     "6s",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			want:     "2m 30s",
		},
		{
			name:     "minutes only (no seconds)",
			duration: 5 * time.Minute,
			want:     "5m",
		},
		{
			name:     "hours minutes and seconds",
			duration: 1*time.Hour + 30*time.Minute + 45*time.Second,
			want:     "1h 30m 45s",
		},
		{
			name:     "hours only",
			duration: 2 * time.Hour,
			want:     "2h",
		},
		{
			name:     "large duration",
			duration: 3*time.Hour + 45*time.Minute + 15*time.Second,
			want:     "3h 45m 15s",
		},
		{
			name:     "exactly one minute",
			duration: 1 * time.Minute,
			want:     "1m",
		},
		{
			name:     "exactly one hour",
			duration: 1 * time.Hour,
			want:     "1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

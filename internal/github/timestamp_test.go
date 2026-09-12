package github

import (
	"testing"
	"time"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:  "valid UTC timestamp",
			input: "2024-06-01T12:34:56Z",
			want:  time.Date(2024, 6, 1, 12, 34, 56, 0, time.UTC),
		},
		{
			name:  "epoch",
			input: "1970-01-01T00:00:00Z",
			want:  time.Unix(0, 0).UTC(),
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "missing zone",
			input:   "2024-06-01T12:34:56",
			wantErr: true,
		},
		{
			name:    "numeric offset instead of Z",
			input:   "2024-06-01T12:34:56+02:00",
			wantErr: true,
		},
		{
			name:    "leap second is rejected by time.Parse",
			input:   "2024-06-01T12:34:60Z",
			wantErr: true,
		},
		{
			name:    "trailing junk",
			input:   "2024-06-01T12:34:56ZZ",
			wantErr: true,
		},
		{
			// Fuzzing found that time.Parse silently accepts and drops
			// fractional seconds even when the layout has none; the
			// round-trip guard in ParseTimestamp now rejects this input.
			name:    "fractional seconds dropped by layout",
			input:   "2024-06-01T12:34:56.5Z",
			wantErr: true,
		},
		{
			name:    "comma decimal separator",
			input:   "2024-06-01T12:34:56,5Z",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimestamp(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTimestamp(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseTimestamp(%q) unexpected error: %v", tt.input, err)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("ParseTimestamp(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// FuzzParseTimestamp checks the REST timestamp parser whose output feeds PR
// age display (internal/tui). Runs as a seed-corpus unit test under plain
// `go test ./...`; real fuzzing is opt-in via `just fuzz`.
// Invariant: a successful parse must round-trip — formatting the result with
// TimestampFormat must reproduce the input exactly. Without the round-trip
// guard, time.Parse silently accepted fractional seconds ("...56.5Z") and
// dropped the fraction.
func FuzzParseTimestamp(f *testing.F) {
	seeds := []string{
		"2024-06-01T12:34:56Z",
		"1970-01-01T00:00:00Z",
		"0001-01-01T00:00:00Z",
		"",
		"2024-06-01T12:34:56",
		"2024-06-01T12:34:56+02:00",
		"2024-06-01T12:34:60Z",
		"2024-06-01T12:34:56.5Z",
		"2024-06-01T12:34:56.123456789Z",
		"2024-06-01T12:34:56,5Z",
		"2024-13-01T12:34:56Z",
		"2024-06-00T12:34:56Z",
		"99999-06-01T12:34:56Z",
		"2024-06-01T12:34:56ZZ",
		" 2024-06-01T12:34:56Z",
		"2024-06-01t12:34:56z",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		parsed, err := ParseTimestamp(s)
		if err != nil {
			return
		}
		if got := parsed.Format(TimestampFormat); got != s {
			t.Errorf("ParseTimestamp(%q) does not round-trip: Format() = %q", s, got)
		}
	})
}
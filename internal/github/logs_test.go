package github

import (
	"strings"
	"testing"
)

func TestParseJobIDFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    int64
		wantErr bool
	}{
		{
			name: "singular job URL",
			url:  "https://github.com/owner/repo/actions/runs/12345678/job/98765432",
			want: 98765432,
		},
		{
			name: "plural jobs URL",
			url:  "https://github.com/owner/repo/actions/runs/12345678/jobs/98765432",
			want: 98765432,
		},
		{
			name:    "no job ID in URL",
			url:     "https://github.com/owner/repo/actions/runs/12345678",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJobIDFromURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseJobIDFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseJobIDFromURL() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantText  string
		wantLevel string
		wantNil   bool
	}{
		{
			name:      "plain line with timestamp",
			input:     "2026-03-26T01:30:00.0000000Z some log output",
			wantText:  "some log output",
			wantLevel: "info",
		},
		{
			name:      "error marker",
			input:     "2026-03-26T01:30:00.0000000Z ##[error]something failed",
			wantText:  "something failed",
			wantLevel: "error",
		},
		{
			name:      "warning marker",
			input:     "2026-03-26T01:30:00.0000000Z ##[warning]node 20 deprecated",
			wantText:  "node 20 deprecated",
			wantLevel: "warning",
		},
		{
			name:      "command marker",
			input:     "2026-03-26T01:30:00.0000000Z ##[command]go test ./...",
			wantText:  "go test ./...",
			wantLevel: "command",
		},
		{
			name:      "section marker",
			input:     "2026-03-26T01:30:00.0000000Z ##[section]Run tests",
			wantText:  "── Run tests",
			wantLevel: "info",
		},
		{
			name:    "group marker skipped",
			input:   "2026-03-26T01:30:00.0000000Z ##[group]Setup",
			wantNil: true,
		},
		{
			name:    "endgroup marker skipped",
			input:   "2026-03-26T01:30:00.0000000Z ##[endgroup]",
			wantNil: true,
		},
		{
			name:    "debug marker skipped",
			input:   "2026-03-26T01:30:00.0000000Z ##[debug]internal debug info",
			wantNil: true,
		},
		{
			name:    "empty line skipped",
			input:   "2026-03-26T01:30:00.0000000Z ",
			wantNil: true,
		},
		{
			name:    "blank line skipped",
			input:   "",
			wantNil: true,
		},
		{
			name:      "line without timestamp",
			input:     "some output without timestamp",
			wantText:  "some output without timestamp",
			wantLevel: "info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLogLine(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseLogLine() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("parseLogLine() = nil, want non-nil")
			}
			if got.Text != tt.wantText {
				t.Errorf("parseLogLine().Text = %q, want %q", got.Text, tt.wantText)
			}
			if got.Level != tt.wantLevel {
				t.Errorf("parseLogLine().Level = %q, want %q", got.Level, tt.wantLevel)
			}
		})
	}
}

func TestParseLastNLines(t *testing.T) {
	makeLog := func(lines ...string) string {
		return strings.Join(lines, "\n")
	}

	t.Run("fewer lines than N", func(t *testing.T) {
		input := makeLog(
			"2026-03-26T01:30:00.0000000Z line one",
			"2026-03-26T01:30:01.0000000Z line two",
		)
		got := parseLastNLines(strings.NewReader(input), 5)
		if len(got) != 2 {
			t.Fatalf("got %d lines, want 2", len(got))
		}
		if got[0].Text != "line one" {
			t.Errorf("got[0].Text = %q, want %q", got[0].Text, "line one")
		}
		if got[1].Text != "line two" {
			t.Errorf("got[1].Text = %q, want %q", got[1].Text, "line two")
		}
	})

	t.Run("exactly N lines", func(t *testing.T) {
		input := makeLog(
			"2026-03-26T01:30:00.0000000Z alpha",
			"2026-03-26T01:30:01.0000000Z beta",
			"2026-03-26T01:30:02.0000000Z gamma",
		)
		got := parseLastNLines(strings.NewReader(input), 3)
		if len(got) != 3 {
			t.Fatalf("got %d lines, want 3", len(got))
		}
		if got[2].Text != "gamma" {
			t.Errorf("last line = %q, want %q", got[2].Text, "gamma")
		}
	})

	t.Run("more lines than N returns last N in order", func(t *testing.T) {
		input := makeLog(
			"2026-03-26T01:30:00.0000000Z first",
			"2026-03-26T01:30:01.0000000Z second",
			"2026-03-26T01:30:02.0000000Z third",
			"2026-03-26T01:30:03.0000000Z fourth",
			"2026-03-26T01:30:04.0000000Z fifth",
			"2026-03-26T01:30:05.0000000Z sixth",
		)
		got := parseLastNLines(strings.NewReader(input), 3)
		if len(got) != 3 {
			t.Fatalf("got %d lines, want 3", len(got))
		}
		if got[0].Text != "fourth" {
			t.Errorf("got[0].Text = %q, want %q", got[0].Text, "fourth")
		}
		if got[1].Text != "fifth" {
			t.Errorf("got[1].Text = %q, want %q", got[1].Text, "fifth")
		}
		if got[2].Text != "sixth" {
			t.Errorf("got[2].Text = %q, want %q", got[2].Text, "sixth")
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		got := parseLastNLines(strings.NewReader(""), 5)
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("skipped lines not counted", func(t *testing.T) {
		input := makeLog(
			"2026-03-26T01:30:00.0000000Z ##[group]Setup",
			"2026-03-26T01:30:01.0000000Z real line",
			"2026-03-26T01:30:02.0000000Z ##[endgroup]",
		)
		got := parseLastNLines(strings.NewReader(input), 5)
		if len(got) != 1 {
			t.Fatalf("got %d lines, want 1", len(got))
		}
		if got[0].Text != "real line" {
			t.Errorf("got[0].Text = %q, want %q", got[0].Text, "real line")
		}
	})
}

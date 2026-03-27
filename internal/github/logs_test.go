package github

import (
	"strings"
	"testing"
)

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantText  string
		wantStyle string
	}{
		{
			name:      "error marker",
			input:     "2026-03-25T18:00:00.0000000Z ##[error]Build failed",
			wantText:  "Build failed",
			wantStyle: "error",
		},
		{
			name:      "warning marker",
			input:     "2026-03-25T18:00:00.0000000Z ##[warning]Deprecated API used",
			wantText:  "Deprecated API used",
			wantStyle: "warning",
		},
		{
			name:      "notice marker",
			input:     "2026-03-25T18:00:00.0000000Z ##[notice]New version available",
			wantText:  "New version available",
			wantStyle: "notice",
		},
		{
			name:      "debug marker",
			input:     "2026-03-25T18:00:00.0000000Z ##[debug]Variable: foo",
			wantText:  "Variable: foo",
			wantStyle: "debug",
		},
		{
			name:      "group marker",
			input:     "2026-03-25T18:00:00.0000000Z ##[group]Run tests",
			wantText:  "Run tests",
			wantStyle: "group",
		},
		{
			name:      "endgroup marker - should be empty",
			input:     "2026-03-25T18:00:00.0000000Z ##[endgroup]",
			wantText:  "",
			wantStyle: "",
		},
		{
			name:      "regular line",
			input:     "2026-03-25T18:00:00.0000000Z Running npm install",
			wantText:  "Running npm install",
			wantStyle: "default",
		},
		{
			name:      "line without timestamp",
			input:     "Build completed successfully",
			wantText:  "Build completed successfully",
			wantStyle: "default",
		},
		{
			name:      "empty line",
			input:     "2026-03-25T18:00:00.0000000Z   ",
			wantText:  "",
			wantStyle: "",
		},
		{
			name:      "long line truncation",
			input:     "2026-03-25T18:00:00.0000000Z " + strings.Repeat("a", 250),
			wantText:  strings.Repeat("a", 197) + "...",
			wantStyle: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLogLine(tt.input)
			if got.Text != tt.wantText {
				t.Errorf("parseLogLine().Text = %q, want %q", got.Text, tt.wantText)
			}
			if got.Style != tt.wantStyle {
				t.Errorf("parseLogLine().Style = %q, want %q", got.Style, tt.wantStyle)
			}
		})
	}
}

func TestParseLastNLines(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		n       int
		wantLen int
		wantErr bool
	}{
		{
			name:    "empty input",
			input:   "",
			n:       5,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "fewer lines than n",
			input:   "line1\nline2\nline3",
			n:       5,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "exactly n lines",
			input:   "line1\nline2\nline3",
			n:       3,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "more lines than n",
			input:   "line1\nline2\nline3\nline4\nline5\nline6\nline7",
			n:       3,
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "with error markers",
			input:   "2026-03-25T18:00:00Z ##[error]Failed\n2026-03-25T18:00:01Z Running tests",
			n:       5,
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "last n lines are endgroup markers - should return earlier content",
			input: "2026-03-26T18:00:00Z Real output line\n" +
				"2026-03-26T18:00:01Z ##[endgroup]\n" +
				"2026-03-26T18:00:02Z ##[endgroup]\n" +
				"2026-03-26T18:00:03Z ##[endgroup]\n" +
				"2026-03-26T18:00:04Z ##[endgroup]\n" +
				"2026-03-26T18:00:05Z ##[endgroup]",
			n:       5,
			wantLen: 1,
			wantErr: false,
		},
		{
			name:    "invalid n",
			input:   "line1\nline2",
			n:       0,
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			got, err := parseLastNLines(reader, tt.n)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLastNLines() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.wantLen {
				t.Errorf("parseLastNLines() returned %d lines, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestParseLastNLinesReturnsCorrectLines(t *testing.T) {
	input := "line1\nline2\nline3\nline4\nline5\nline6\nline7"
	reader := strings.NewReader(input)

	lines, err := parseLastNLines(reader, 3)
	if err != nil {
		t.Fatalf("parseLastNLines() error = %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Should return last 3 lines (line5, line6, line7)
	expected := []string{"line5", "line6", "line7"}
	for i, line := range lines {
		if line.Text != expected[i] {
			t.Errorf("line[%d].Text = %q, want %q", i, line.Text, expected[i])
		}
	}
}

func TestParseJobIDFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  int64
		wantErr bool
	}{
		{
			name:    "job URL",
			url:     "https://github.com/owner/repo/actions/runs/12345/job/67890",
			wantID:  67890,
			wantErr: false,
		},
		{
			name:    "jobs URL",
			url:     "https://github.com/owner/repo/actions/runs/12345/jobs/67890",
			wantID:  67890,
			wantErr: false,
		},
		{
			name:    "URL with query params",
			url:     "https://github.com/owner/repo/actions/runs/12345/job/67890?pr=42",
			wantID:  67890,
			wantErr: false,
		},
		{
			name:    "invalid URL - no job ID",
			url:     "https://github.com/owner/repo/actions/runs/12345",
			wantID:  0,
			wantErr: true,
		},
		{
			name:    "invalid URL - malformed job ID",
			url:     "https://github.com/owner/repo/actions/runs/12345/job/abc",
			wantID:  0,
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
			if got != tt.wantID {
				t.Errorf("ParseJobIDFromURL() = %d, want %d", got, tt.wantID)
			}
		})
	}
}

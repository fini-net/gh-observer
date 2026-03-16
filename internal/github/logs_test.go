package github

import (
	"strings"
	"testing"
)

func TestParseErrorLines(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantLen     int
		wantContain []string
	}{
		{
			name:        "extracts ##[error] lines",
			input:       "2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 127.\nSome other line\n",
			wantLen:     1,
			wantContain: []string{"Process completed with exit code 127"},
		},
		{
			name: "extracts shell errors before ##[error]",
			input: `2026-03-16T18:56:23.0419487Z /home/runner/work/_temp/abc.sh: line 1: magick: command not found
2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 127.
`,
			wantLen:     2,
			wantContain: []string{"line 1: magick: command not found", "exit code 127"},
		},
		{
			name:    "filters post job cleanup errors",
			input:   "2026-03-16T18:56:23.0425787Z ##[error]Post job cleanup failed\n",
			wantLen: 0,
		},
		{
			name:        "limits to 3 errors",
			input:       "##[error]Error 1\n##[error]Error 2\n##[error]Error 3\n##[error]Error 4\n",
			wantLen:     3,
			wantContain: []string{"Error 1", "Error 2"},
		},
		{
			name:    "no error lines",
			input:   "Regular log output without errors\n",
			wantLen: 0,
		},
		{
			name: "command not found detected before ##[error]",
			input: `/home/runner/work/_temp/script.sh: line 1: magick: command not found
##[error]Process completed with exit code 127.
`,
			wantLen:     2,
			wantContain: []string{"command not found", "exit code 127"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseErrorLines(strings.NewReader(tt.input))
			if len(got) != tt.wantLen {
				t.Errorf("parseErrorLines() returned %d lines, want %d", len(got), tt.wantLen)
			}
			for _, want := range tt.wantContain {
				found := false
				for _, line := range got {
					if strings.Contains(line, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("parseErrorLines() missing expected content %q in %v", want, got)
				}
			}
		})
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "standard error message",
			line: "2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 127.",
			want: "Process completed with exit code 127.",
		},
		{
			name: "filters post job cleanup",
			line: "##[error]Post job cleanup: something failed",
			want: "",
		},
		{
			name: "no error marker",
			line: "Regular log line without error",
			want: "",
		},
		{
			name: "truncates long messages",
			line: "##[error]" + strings.Repeat("x", 250),
			want: strings.Repeat("x", 197) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractErrorMessage(tt.line)
			if got != tt.want {
				t.Errorf("extractErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractShellError(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "command not found with timestamp",
			line: "2026-03-16T18:56:23.0419487Z /home/runner/work/_temp/abc.sh: line 1: magick: command not found",
			want: "line 1: magick: command not found",
		},
		{
			name: "command not found without timestamp",
			line: "/home/runner/work/_temp/script.sh: line 5: foo: command not found",
			want: "line 5: foo: command not found",
		},
		{
			name: "no such file or directory",
			line: "line 2: /path/to/file: No such file or directory",
			want: "line 2: /path/to/file: No such file or directory",
		},
		{
			name: "permission denied",
			line: "/tmp/script.sh: line 3: ./bin/app: Permission denied",
			want: "line 3: ./bin/app: Permission denied",
		},
		{
			name: "regular log line",
			line: "2026-03-16T18:56:23Z Running tests...",
			want: "",
		},
		{
			name: "empty line",
			line: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractShellError(tt.line)
			if got != tt.want {
				t.Errorf("extractShellError() = %q, want %q", got, tt.want)
			}
		})
	}
}

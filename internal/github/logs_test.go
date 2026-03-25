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
		{
			name: "context line before generic exit code error",
			input: `2026-03-16T18:56:23.0419487Z Linting blocked: commit message contains '[wip|fixup|no_ci]'.
2026-03-16T18:56:23.0425787Z Error: Process completed with exit code 1.
2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 1.
`,
			wantLen:     2,
			wantContain: []string{"Linting blocked", "exit code 1"},
		},
		{
			name: "filters Run command echo before exit code",
			input: `Run if git log --pretty=%B origin/main..HEAD | grep -Eiq 'wip|fixup|no_ci'; then
2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 1.
`,
			wantLen:     1,
			wantContain: []string{"exit code 1"},
		},
		{
			name: "captures meaningful line after Run echo",
			input: `Run if git log --pretty=%B origin/main..HEAD | grep -Eiq 'wip|fixup|no_ci'; then
Linting blocked: commit message contains '[wip|fixup|no_ci]'.
2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 1.
`,
			wantLen:     2,
			wantContain: []string{"Linting blocked", "exit code 1"},
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

func TestIsGenericExitCodeError(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "generic exit code error",
			msg:  "Process completed with exit code 1.",
			want: true,
		},
		{
			name: "generic exit code 127",
			msg:  "Process completed with exit code 127.",
			want: true,
		},
		{
			name: "specific error",
			msg:  "Some specific error message",
			want: false,
		},
		{
			name: "empty string",
			msg:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGenericExitCodeError(tt.msg); got != tt.want {
				t.Errorf("isGenericExitCodeError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNoiseLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "empty line",
			line: "",
			want: true,
		},
		{
			name: "whitespace only",
			line: "   ",
			want: true,
		},
		{
			name: "Run if command",
			line: "Run if git log --pretty=%B origin/main..HEAD | grep -Eiq 'wip|fixup|no_ci'; then",
			want: true,
		},
		{
			name: "shell line",
			line: "shell: /usr/bin/bash -e {0}",
			want: true,
		},
		{
			name: "meaningful error line",
			line: "Linting blocked: commit message contains '[wip|fixup|no_ci]'.",
			want: false,
		},
		{
			name: "env line",
			line: "env:",
			want: false,
		},
		{
			name: "group marker",
			line: "##[group]Some group",
			want: true,
		},
		{
			name: "regular output",
			line: "Running tests...",
			want: false,
		},
		{
			name: "Error echo before ##[error]",
			line: "Error: Process completed with exit code 1.",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNoiseLine(tt.line); got != tt.want {
				t.Errorf("isNoiseLine() = %v, want %v", got, tt.want)
			}
		})
	}
}

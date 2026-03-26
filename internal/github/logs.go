package github

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-github/v84/github"
)

const (
	maxLogLineLen = 200
)

// LogLine represents a parsed log line with style metadata
type LogLine struct {
	Text  string
	Style string
}

// FetchLastNJobLines fetches the last N lines from a job's logs.
// Lines are truncated to maxLogLineLen characters, timestamp prefixes are stripped,
// and GitHub Actions markers are converted to style metadata.
func FetchLastNJobLines(ctx context.Context, client *github.Client, owner, repo string, jobID int64, n int) ([]LogLine, error) {
	logURL, _, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 0)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL.String(), nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch job logs: HTTP %s", resp.Status)
	}

	return parseLastNLines(resp.Body, n)
}

// parseLastNLines extracts the last N lines from log output with style metadata.
// Uses a ring buffer to maintain O(N) memory usage regardless of log size.
func parseLastNLines(reader io.Reader, n int) ([]LogLine, error) {
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive, got %d", n)
	}

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	ring := make([]string, n)
	idx := 0
	count := 0
	wrapped := false

	for scanner.Scan() {
		ring[idx] = scanner.Text()
		idx++
		if idx == n {
			idx = 0
			wrapped = true
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan log lines: %w", err)
	}

	if count == 0 {
		return nil, nil
	}

	resultCount := count
	if resultCount > n {
		resultCount = n
	}

	result := make([]LogLine, 0, resultCount)
	startIdx := 0
	if wrapped {
		startIdx = idx
	}

	for i := 0; i < resultCount; i++ {
		pos := (startIdx + i) % n
		line := ring[pos]
		logLine := parseLogLine(line)
		if logLine.Text != "" {
			result = append(result, logLine)
		}
	}

	return result, nil
}

// parseLogLine extracts style from GitHub Actions markers and strips timestamps.
func parseLogLine(line string) LogLine {
	// Strip timestamp prefix: "2026-03-25T18:00:00.0000000Z <content>"
	if idx := strings.Index(line, "Z "); idx != -1 {
		line = line[idx+2:]
	}

	var style string
	var text string

	switch {
	case strings.Contains(line, "##[error]"):
		style = "error"
		text = strings.TrimSpace(strings.ReplaceAll(line, "##[error]", ""))
	case strings.Contains(line, "##[warning]"):
		style = "warning"
		text = strings.TrimSpace(strings.ReplaceAll(line, "##[warning]", ""))
	case strings.Contains(line, "##[notice]"):
		style = "notice"
		text = strings.TrimSpace(strings.ReplaceAll(line, "##[notice]", ""))
	case strings.Contains(line, "##[group]"):
		style = "group"
		text = strings.TrimSpace(strings.ReplaceAll(line, "##[group]", ""))
	case strings.Contains(line, "##[endgroup]"):
		style = "endgroup"
		text = ""
	case strings.Contains(line, "##[debug]"):
		style = "debug"
		text = strings.TrimSpace(strings.ReplaceAll(line, "##[debug]", ""))
	default:
		style = "default"
		text = strings.TrimSpace(line)
	}

	// Skip empty lines and endgroup markers
	if text == "" {
		return LogLine{}
	}

	// Truncate very long lines
	if len(text) > maxLogLineLen {
		text = text[:maxLogLineLen-3] + "..."
	}

	return LogLine{Text: text, Style: style}
}

// ParseJobIDFromURL extracts the job ID from a GitHub Actions details URL.
func ParseJobIDFromURL(detailsURL string) (int64, error) {
	// URLs are like: https://github.com/owner/repo/actions/runs/12345/job/67890
	// or: https://github.com/owner/repo/actions/runs/12345/jobs/67890
	parts := strings.Split(detailsURL, "/")
	for i, part := range parts {
		if (part == "job" || part == "jobs") && i+1 < len(parts) {
			var jobID int64
			_, err := fmt.Sscanf(parts[i+1], "%d", &jobID)
			if err != nil {
				return 0, fmt.Errorf("failed to parse job ID from URL: %w", err)
			}
			return jobID, nil
		}
	}
	return 0, fmt.Errorf("no job ID found in URL: %s", detailsURL)
}

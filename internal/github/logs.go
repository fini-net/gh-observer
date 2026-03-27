package github

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	gogithub "github.com/google/go-github/v84/github"
)

var jobIDRegexp = regexp.MustCompile(`/actions/runs/\d+/jobs?/(\d+)`)
var timestampRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z `)
var levelRegexp = regexp.MustCompile(`^##\[(\w+)\](.*)`)

// LogLine represents a parsed log line with severity metadata.
type LogLine struct {
	Text  string
	Level string // "error", "warning", "command", "info"
}

// ParseJobIDFromURL extracts the job ID from a GitHub Actions DetailsURL.
func ParseJobIDFromURL(detailsURL string) (int64, error) {
	matches := jobIDRegexp.FindStringSubmatch(detailsURL)
	if len(matches) < 2 {
		return 0, fmt.Errorf("no job ID found in URL: %s", detailsURL)
	}
	return strconv.ParseInt(matches[1], 10, 64)
}

// FetchLastNJobLines fetches the last n log lines from a GitHub Actions job.
// It uses a ring buffer for O(n) memory usage regardless of log size.
// Non-fatal: returns nil, nil if the job has no logs yet.
func FetchLastNJobLines(ctx context.Context, client *gogithub.Client, owner, repo string, jobID int64, n int) ([]LogLine, error) {
	logURL, ghResp, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 0)
	if err != nil {
		if ghResp != nil && ghResp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get job log URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build log request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch job logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	return parseLastNLines(resp.Body, n), nil
}

// parseLastNLines reads from r and returns the last n non-empty log lines.
func parseLastNLines(r io.Reader, n int) []LogLine {
	if n <= 0 {
		return nil
	}

	ring := make([]LogLine, n)
	idx := 0

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := parseLogLine(scanner.Text())
		if line == nil {
			continue
		}
		ring[idx%n] = *line
		idx++
	}

	if idx == 0 {
		return nil
	}

	count := min(idx, n)
	result := make([]LogLine, count)
	start := 0
	if idx > n {
		start = idx % n
	}
	for i := range result {
		result[i] = ring[(start+i)%n]
	}
	return result
}

// parseLogLine parses a raw log line, stripping the timestamp prefix and
// extracting the severity level from GitHub Actions markers (##[level]).
// Returns nil for lines that should be skipped (empty, debug, group markers).
func parseLogLine(raw string) *LogLine {
	text := timestampRegexp.ReplaceAllString(raw, "")

	level := "info"
	if m := levelRegexp.FindStringSubmatch(text); m != nil {
		marker := strings.ToLower(m[1])
		text = m[2]
		switch marker {
		case "error":
			level = "error"
		case "warning":
			level = "warning"
		case "command":
			level = "command"
		case "section":
			text = "── " + text
		case "group", "endgroup", "debug":
			return nil
		default:
			level = "info"
		}
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	return &LogLine{Text: text, Level: level}
}

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
	maxSlowLogLineLen = 200
)

// FetchJobLogs retrieves job logs and extracts relevant error lines.
// Returns up to 3 most relevant error lines from the logs.
func FetchJobLogs(ctx context.Context, client *github.Client, owner, repo string, jobID int64) ([]string, error) {
	// Get the redirect URL for the logs
	logURL, _, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 0)
	if err != nil {
		return nil, err
	}

	// Create an HTTP client for following the redirect
	// We use a fresh client without auth for the actual log download (logs are public)
	httpClient := &http.Client{}
	resp, err := httpClient.Get(logURL.String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	return parseErrorLines(resp.Body), nil
}

// parseErrorLines extracts relevant error lines from job logs.
// It looks for ##[error] markers AND shell errors (command not found, etc.).
func parseErrorLines(reader io.Reader) []string {
	scanner := bufio.NewScanner(reader)
	var errorLines []string
	seen := make(map[string]bool)
	var prevLine string
	var prevPrevLine string

	for scanner.Scan() {
		line := scanner.Text()

		// Look for ##[error] lines
		if strings.Contains(line, "##[error]") {
			// Extract the error message after ##[error]
			errorMsg := extractErrorMessage(line)
			if errorMsg != "" && !seen[errorMsg] {
				seen[errorMsg] = true
				errorLines = append(errorLines, errorMsg)
			}

			// Check previous line for context
			if prevLine != "" {
				// For generic exit code errors, capture meaningful preceding line
				if isGenericExitCodeError(errorMsg) {
					if ctx := extractMeaningfulContext(prevLine, prevPrevLine); ctx != "" && !seen[ctx] {
						seen[ctx] = true
						errorLines = append([]string{ctx}, errorLines...)
					}
				} else {
					// For specific shell errors, use the existing pattern matching
					if shellErr := extractShellError(prevLine); shellErr != "" && !seen[shellErr] {
						seen[shellErr] = true
						errorLines = append([]string{shellErr}, errorLines...)
					}
				}
			}
		}

		prevPrevLine = prevLine
		prevLine = line

		// Limit to 3 unique error lines
		if len(errorLines) >= 3 {
			break
		}
	}

	return errorLines
}

// isGenericExitCodeError checks if the error is just a generic exit code wrapper
func isGenericExitCodeError(msg string) bool {
	return strings.HasPrefix(msg, "Process completed with exit code")
}

// extractMeaningfulContext finds a meaningful error context line
func extractMeaningfulContext(prevLine, prevPrevLine string) string {
	// First try the immediate previous line
	if ctx := extractContextLine(prevLine); ctx != "" {
		return ctx
	}
	// Fall back to the line before that
	return extractContextLine(prevPrevLine)
}

// extractContextLine extracts meaningful context from a log line
func extractContextLine(line string) string {
	// Strip timestamp prefix if present
	if idx := strings.Index(line, "Z "); idx != -1 {
		line = line[idx+2:]
	}

	// Filter out noise
	if isNoiseLine(line) {
		return ""
	}

	// Truncate very long lines
	if len(line) > 200 {
		line = line[:197] + "..."
	}

	return strings.TrimSpace(line)
}

// isNoiseLine detects lines that are just noise (not meaningful error context)
func isNoiseLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}

	// Generic error echoes (GitHub Actions echoes these before ##[error])
	if strings.HasPrefix(line, "Error: Process completed with exit code") {
		return true
	}

	// Shell command echoes (lines starting with "Run " followed by shell commands)
	if strings.HasPrefix(line, "Run ") && (strings.HasPrefix(line, "Run if ") || strings.HasPrefix(line, "Run cd ") || strings.HasPrefix(line, "Run export ") || strings.HasPrefix(line, "Run echo ") || strings.HasPrefix(line, "Run npm ") || strings.HasPrefix(line, "Run yarn ") || strings.HasPrefix(line, "Run pip ")) {
		return true
	}

	// Environment variable display lines (key=value without context)
	if strings.HasPrefix(line, "env:") {
		return false
	}

	// Common noise patterns
	noisePatterns := []string{
		"shell: /usr/bin/bash",
		"permissions:",
		"##[endgroup]",
		"##[group]",
	}
	for _, pattern := range noisePatterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}

	return false
}

// extractShellError detects shell/binary errors in log lines (not marked with ##[error])
func extractShellError(line string) string {
	// Strip timestamp prefix if present
	// Format: "2026-03-16T18:56:23.0419487Z <actual content>"
	if idx := strings.Index(line, "Z "); idx != -1 {
		line = line[idx+2:]
	}

	// Look for shell script error patterns
	// Pattern 1: "/path/to/script.sh: line N: command: error message"
	// Pattern 2: "line N: command: error message"
	if strings.Contains(line, "command not found") ||
		strings.Contains(line, "No such file or directory") ||
		strings.Contains(line, "Permission denied") ||
		strings.Contains(line, "cannot find") ||
		strings.Contains(line, "not a valid identifier") {
		// Extract just the error part
		if idx := strings.Index(line, "line "); idx != -1 {
			return strings.TrimSpace(line[idx:])
		}
		return strings.TrimSpace(line)
	}

	return ""
}

// extractErrorMessage parses the error message from a log line.
// Lines are typically: "2026-03-16T18:56:23.0425787Z ##[error]Process completed with exit code 127."
func extractErrorMessage(line string) string {
	_, after, ok := strings.Cut(line, "##[error]")
	if !ok {
		return ""
	}

	msg := strings.TrimSpace(after)

	// Filter out noise from cleanup/post-job phases
	if strings.Contains(msg, "Post job cleanup") {
		return ""
	}

	// Truncate very long messages
	if len(msg) > 200 {
		msg = msg[:197] + "..."
	}

	return msg
}

// ExtractJobIDFromDetailsURL extracts the job ID from a GitHub Actions details URL.
func ExtractJobIDFromDetailsURL(detailsURL string) (int64, error) {
	return ParseJobIDFromURL(detailsURL)
}

// FetchLastNJobLines fetches the last N lines from a job's logs.
// Lines are truncated to maxSlowLogLineLen characters and timestamp prefixes are stripped.
func FetchLastNJobLines(ctx context.Context, client *github.Client, owner, repo string, jobID int64, n int) ([]string, error) {
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

// parseLastNLines extracts the last N lines from log output, with cleanup.
// Uses a ring buffer to maintain O(N) memory usage regardless of log size.
func parseLastNLines(reader io.Reader, n int) ([]string, error) {
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

	result := make([]string, 0, resultCount)
	startIdx := 0
	if wrapped {
		startIdx = idx
	}

	for i := 0; i < resultCount; i++ {
		pos := (startIdx + i) % n
		line := ring[pos]
		if idx := strings.Index(line, "Z "); idx != -1 {
			line = line[idx+2:]
		}
		if len(line) > maxSlowLogLineLen {
			line = line[:maxSlowLogLineLen-3] + "..."
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result, nil
}

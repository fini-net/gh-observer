package debug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogNoopWhenDisabled(t *testing.T) {
	enabled = false
	logFile = nil

	Log("test message", "key", "value")

	// No panic or error means noop worked
}

func TestEnableCreatesFile(t *testing.T) {
	enabled = false
	logFile = nil

	err := Enable()
	if err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}
	defer Close()

	if !enabled {
		t.Error("expected enabled to be true after Enable()")
	}

	path := LogPath()
	if path == "" {
		t.Error("expected non-empty log path after Enable()")
	}

	dir := filepath.Dir(path)
	if !strings.Contains(dir, "gh-observer-debug") {
		t.Errorf("expected path to contain 'gh-observer-debug', got: %s", path)
	}

	// Verify the file exists
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("expected log file to exist: %s, err: %v", path, statErr)
	}

	// Verify the file has content
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("failed to read log file: %v", readErr)
	}
	if !strings.Contains(string(content), "debug logging started") {
		t.Errorf("expected log to contain 'debug logging started', got: %s", string(content))
	}
}

func TestLogWritesToFile(t *testing.T) {
	enabled = false
	logFile = nil

	err := Enable()
	if err != nil {
		t.Fatalf("Enable() failed: %v", err)
	}
	defer Close()

	Log("test message", "key", "value")

	// Flush by closing
	path := LogPath()
	Close()

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("failed to read log file: %v", readErr)
	}
	if !strings.Contains(string(content), "test message") {
		t.Errorf("expected log to contain 'test message', got: %s", string(content))
	}
}

func TestCloseResetsState(t *testing.T) {
	enabled = false
	logFile = nil

	Enable()
	path := LogPath()

	Close()

	if enabled {
		t.Error("expected enabled to be false after Close()")
	}
	if LogPath() != "" {
		t.Error("expected empty log path after Close()")
	}

	// Log should be noop after close
	Log("should not appear", "key", "value")

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("failed to read log file: %v", readErr)
	}
	if strings.Contains(string(content), "should not appear") {
		t.Error("expected no output after Close()")
	}

	os.Remove(path)
}

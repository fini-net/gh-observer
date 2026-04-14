package debug

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

var enabled bool
var logFile *os.File

func Enable() error {
	dir := filepath.Join(os.TempDir(), "gh-observer-debug")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create debug log directory: %w", err)
	}

	ts := time.Now().Format("2006-01-02_150405")
	path := filepath.Join(dir, fmt.Sprintf("debug-%s.log", ts))

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create debug log file: %w", err)
	}

	enabled = true
	logFile = f

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slog.SetDefault(slog.New(handler))

	slog.Debug("debug logging started", "path", path)

	return nil
}

func Enabled() bool {
	return enabled
}

func LogPath() string {
	if logFile == nil {
		return ""
	}
	return logFile.Name()
}

func Close() {
	if logFile != nil {
		slog.Debug("debug logging stopped")
		logFile.Close()
		logFile = nil
	}
	enabled = false
}

func Log(msg string, args ...any) {
	if enabled {
		slog.Debug(msg, args...)
	}
}

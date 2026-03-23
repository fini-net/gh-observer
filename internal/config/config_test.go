package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_DefaultValues(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.RefreshInterval != 5*time.Second {
		t.Errorf("RefreshInterval = %v, want 5s", cfg.RefreshInterval)
	}
	if cfg.Colors.Success != 10 {
		t.Errorf("Colors.Success = %d, want 10", cfg.Colors.Success)
	}
	if cfg.Colors.Failure != 9 {
		t.Errorf("Colors.Failure = %d, want 9", cfg.Colors.Failure)
	}
	if cfg.Colors.Running != 11 {
		t.Errorf("Colors.Running = %d, want 11", cfg.Colors.Running)
	}
	if cfg.Colors.Queued != 8 {
		t.Errorf("Colors.Queued = %d, want 8", cfg.Colors.Queued)
	}
	if cfg.EnableLinks != true {
		t.Errorf("EnableLinks = %v, want true", cfg.EnableLinks)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config", "gh-observer")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `refresh_interval: 30s
enable_links: false
colors:
  success: 2
  failure: 1
  running: 3
  queued: 7
`
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.RefreshInterval != 30*time.Second {
		t.Errorf("RefreshInterval = %v, want 30s", cfg.RefreshInterval)
	}
	if cfg.Colors.Success != 2 {
		t.Errorf("Colors.Success = %d, want 2", cfg.Colors.Success)
	}
	if cfg.Colors.Failure != 1 {
		t.Errorf("Colors.Failure = %d, want 1", cfg.Colors.Failure)
	}
	if cfg.Colors.Running != 3 {
		t.Errorf("Colors.Running = %d, want 3", cfg.Colors.Running)
	}
	if cfg.Colors.Queued != 7 {
		t.Errorf("Colors.Queued = %d, want 7", cfg.Colors.Queued)
	}
	if cfg.EnableLinks != false {
		t.Errorf("EnableLinks = %v, want false", cfg.EnableLinks)
	}
}

func TestLoad_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config", "gh-observer")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := "refresh_interval: 10s\n"
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.RefreshInterval != 10*time.Second {
		t.Errorf("RefreshInterval = %v, want 10s", cfg.RefreshInterval)
	}
	if cfg.Colors.Success != 10 {
		t.Errorf("Colors.Success = %d, want 10 (default)", cfg.Colors.Success)
	}
	if cfg.Colors.Failure != 9 {
		t.Errorf("Colors.Failure = %d, want 9 (default)", cfg.Colors.Failure)
	}
	if cfg.Colors.Running != 11 {
		t.Errorf("Colors.Running = %d, want 11 (default)", cfg.Colors.Running)
	}
	if cfg.Colors.Queued != 8 {
		t.Errorf("Colors.Queued = %d, want 8 (default)", cfg.Colors.Queued)
	}
	if cfg.EnableLinks != true {
		t.Errorf("EnableLinks = %v, want true (default)", cfg.EnableLinks)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".config", "gh-observer")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	invalidConfig := "refresh_interval: \"not a valid duration string\"\n"
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error for invalid YAML, got nil")
	}
}

func TestLoad_DurationParsing(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want time.Duration
	}{
		{
			name: "seconds as string",
			yaml: "refresh_interval: \"10s\"\n",
			want: 10 * time.Second,
		},
		{
			name: "minutes and seconds",
			yaml: "refresh_interval: \"1m30s\"\n",
			want: 90 * time.Second,
		},
		{
			name: "two minutes",
			yaml: "refresh_interval: \"2m\"\n",
			want: 2 * time.Minute,
		},
		{
			name: "without quotes",
			yaml: "refresh_interval: 15s\n",
			want: 15 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("HOME", tmpDir)

			configDir := filepath.Join(tmpDir, ".config", "gh-observer")
			if err := os.MkdirAll(configDir, 0755); err != nil {
				t.Fatalf("failed to create config dir: %v", err)
			}

			configPath := filepath.Join(configDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}

			if cfg.RefreshInterval != tt.want {
				t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, tt.want)
			}
		})
	}
}

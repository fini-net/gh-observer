package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fini-net/gh-observer/internal/debug"
	"github.com/spf13/viper"
)

type ColorConfig struct {
	Success int `mapstructure:"success"`
	Failure int `mapstructure:"failure"`
	Running int `mapstructure:"running"`
	Queued  int `mapstructure:"queued"`
}

type Config struct {
	RefreshInterval     time.Duration    `mapstructure:"refresh_interval"`
	RepoRefreshInterval time.Duration    `mapstructure:"repo_refresh_interval"`
	FadeSuccess         time.Duration    `mapstructure:"fade_success"`
	FadeFailure         time.Duration    `mapstructure:"fade_failure"`
	Colors              ColorConfig      `mapstructure:"colors"`
	EnableLinks         bool            `mapstructure:"enable_links"`
	PresumedAverages    map[string]string `mapstructure:"presumed_averages"`
}

func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("refresh_interval", "5s")
	v.SetDefault("repo_refresh_interval", "30s")
	v.SetDefault("fade_success", "15m")
	v.SetDefault("fade_failure", "30m")
	v.SetDefault("colors.success", 10) // Green
	v.SetDefault("colors.failure", 9)  // Red
	v.SetDefault("colors.running", 11) // Yellow
	v.SetDefault("colors.queued", 8)   // Gray
	v.SetDefault("enable_links", true)
	v.SetDefault("presumed_averages.DCO", "1s")

	// Config location: ~/.config/gh-observer/config.yaml
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("detecting home directory: %w", err)
	}
	configDir := filepath.Join(home, ".config", "gh-observer")
	v.AddConfigPath(configDir)
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Ignore errors if config doesn't exist - we'll use defaults
	if err := v.ReadInConfig(); err != nil {
		debug.Log("config read error", "err", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// PresumedAveragesDurations resolves the raw presumed_averages string map
// into a map of time.Duration keyed by check Name. Invalid duration strings
// are silently dropped (matching the lenient approach used elsewhere in the
// config system). The returned map is never nil when cfg.PresumedAverages is
// populated. The default DCO entry is included automatically via viper
// defaults, so callers can always look up "DCO" in the result.
func (c *Config) PresumedAveragesDurations() map[string]time.Duration {
	if len(c.PresumedAverages) == 0 {
		return nil
	}
	result := make(map[string]time.Duration, len(c.PresumedAverages))
	for name, raw := range c.PresumedAverages {
		d, err := time.ParseDuration(raw)
		if err != nil {
			debug.Log("presumed_averages parse error", "name", name, "value", raw, "err", err)
			continue
		}
		result[name] = d
	}
	return result
}

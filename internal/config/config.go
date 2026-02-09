package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

type ColorConfig struct {
	Success int `mapstructure:"success"`
	Failure int `mapstructure:"failure"`
	Running int `mapstructure:"running"`
	Queued  int `mapstructure:"queued"`
}

type Config struct {
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	Colors          ColorConfig   `mapstructure:"colors"`
}

func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("refresh_interval", "5s")
	v.SetDefault("colors.success", 10) // Green
	v.SetDefault("colors.failure", 9)  // Red
	v.SetDefault("colors.running", 11) // Yellow
	v.SetDefault("colors.queued", 8)   // Gray

	// Config location: ~/.config/gh-observer/config.yaml
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "gh-observer")
	v.AddConfigPath(configDir)
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Ignore errors if config doesn't exist - we'll use defaults
	_ = v.ReadInConfig()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

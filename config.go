package main

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Jellyfin JellyfinConfig `yaml:"jellyfin"`
	Title    string         `yaml:"title"`
	Limit    int            `yaml:"limit"`
}

type JellyfinConfig struct {
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	UserID    string `yaml:"user_id"`
	PublicURL string `yaml:"public_url"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, err
	}

	if cfg.Title == "" {
		cfg.Title = "Library"
	}
	if cfg.Limit == 0 {
		cfg.Limit = 12
	}

	if cfg.Jellyfin.URL == "" {
		return nil, fmt.Errorf("jellyfin.url is required")
	}
	if cfg.Jellyfin.Token == "" {
		return nil, fmt.Errorf("jellyfin.token is required")
	}
	if cfg.Jellyfin.UserID == "" {
		return nil, fmt.Errorf("jellyfin.user_id is required")
	}
	if cfg.Jellyfin.PublicURL == "" {
		return nil, fmt.Errorf("jellyfin.public_url is required")
	}
	if cfg.Limit < 0 {
		return nil, fmt.Errorf("limit must not be negative, got %d", cfg.Limit)
	}

	return &cfg, nil
}

// lookupNonEmptyEnv returns (value, true) only when the environment variable
// is actually set to a non-empty string — matters for GUI-driven deployments
// (e.g. Komodo) where an unfilled-in stack variable is passed through as an
// empty string rather than being absent.
func lookupNonEmptyEnv(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

func applyEnvOverrides(cfg *Config) error {
	if v, ok := lookupNonEmptyEnv("JELLYFIN_URL"); ok {
		cfg.Jellyfin.URL = v
	}
	if v, ok := lookupNonEmptyEnv("JELLYFIN_TOKEN"); ok {
		cfg.Jellyfin.Token = v
	}
	if v, ok := lookupNonEmptyEnv("JELLYFIN_USER_ID"); ok {
		cfg.Jellyfin.UserID = v
	}
	if v, ok := lookupNonEmptyEnv("JELLYFIN_PUBLIC_URL"); ok {
		cfg.Jellyfin.PublicURL = v
	}
	if v, ok := lookupNonEmptyEnv("TITLE"); ok {
		cfg.Title = v
	}
	if v, ok := lookupNonEmptyEnv("LIMIT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("env LIMIT=%q is not a valid integer: %w", v, err)
		}
		cfg.Limit = n
	}
	return nil
}

package main

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Jellyfin JellyfinConfig `yaml:"jellyfin"`
	Radarr   RadarrConfig   `yaml:"radarr"`
	Sonarr   SonarrConfig   `yaml:"sonarr"`
	// PublicURL is THIS service's own path/origin, reachable from the
	// browser — used to prefix the poster <img src="..."> URLs so they
	// reach this container through a reverse proxy, exactly like
	// jellyfin.public_url is Jellyfin's own browser-facing URL for
	// click-through links. Not required: "" means this service is served
	// from the site root.
	PublicURL        string `yaml:"public_url"`
	Title            string `yaml:"title"`
	Limit            int    `yaml:"limit"`
	DownloadingLimit int    `yaml:"downloading_limit"`
}

type JellyfinConfig struct {
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	UserID    string `yaml:"user_id"`
	PublicURL string `yaml:"public_url"`
}

// RadarrConfig and SonarrConfig are reachable only from this container, not
// the browser — unlike Jellyfin, nothing here needs a public_url
// counterpart (see the plan's Global Constraints).
type RadarrConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type SonarrConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
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
	if cfg.DownloadingLimit == 0 {
		cfg.DownloadingLimit = 12
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
	if cfg.Radarr.URL == "" {
		return nil, fmt.Errorf("radarr.url is required")
	}
	if cfg.Radarr.Token == "" {
		return nil, fmt.Errorf("radarr.token is required")
	}
	if cfg.Sonarr.URL == "" {
		return nil, fmt.Errorf("sonarr.url is required")
	}
	if cfg.Sonarr.Token == "" {
		return nil, fmt.Errorf("sonarr.token is required")
	}
	if cfg.Limit < 0 {
		return nil, fmt.Errorf("limit must not be negative, got %d", cfg.Limit)
	}
	if cfg.DownloadingLimit < 0 {
		return nil, fmt.Errorf("downloading_limit must not be negative, got %d", cfg.DownloadingLimit)
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
	if v, ok := lookupNonEmptyEnv("RADARR_URL"); ok {
		cfg.Radarr.URL = v
	}
	if v, ok := lookupNonEmptyEnv("RADARR_TOKEN"); ok {
		cfg.Radarr.Token = v
	}
	if v, ok := lookupNonEmptyEnv("SONARR_URL"); ok {
		cfg.Sonarr.URL = v
	}
	if v, ok := lookupNonEmptyEnv("SONARR_TOKEN"); ok {
		cfg.Sonarr.Token = v
	}
	if v, ok := lookupNonEmptyEnv("PUBLIC_URL"); ok {
		cfg.PublicURL = v
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
	if v, ok := lookupNonEmptyEnv("DOWNLOADING_LIMIT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("env DOWNLOADING_LIMIT=%q is not a valid integer: %w", v, err)
		}
		cfg.DownloadingLimit = n
	}
	return nil
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func setEnv(t *testing.T, name, value string) {
	t.Helper()
	prev, had := os.LookupEnv(name)
	if err := os.Setenv(name, value); err != nil {
		t.Fatalf("setenv %s: %v", name, err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(name, prev)
		} else {
			os.Unsetenv(name)
		}
	})
}

func TestLoadConfig_Defaults(t *testing.T) {
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Title != "Library" {
		t.Errorf("Title = %q, want %q", cfg.Title, "Library")
	}
	if cfg.Limit != 12 {
		t.Errorf("Limit = %d, want 12", cfg.Limit)
	}
	if cfg.Jellyfin.URL != "http://jellyfin:8096" {
		t.Errorf("Jellyfin.URL = %q, want http://jellyfin:8096", cfg.Jellyfin.URL)
	}
	if cfg.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty (no forced default — site root)", cfg.PublicURL)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	setEnv(t, "JELLYFIN_URL", "http://jellyfin.internal:8096")
	setEnv(t, "JELLYFIN_TOKEN", "secret-token")
	setEnv(t, "JELLYFIN_USER_ID", "env-user")
	setEnv(t, "JELLYFIN_PUBLIC_URL", "https://jf.example.com")
	setEnv(t, "TITLE", "Recently Added")
	setEnv(t, "LIMIT", "20")
	setEnv(t, "PUBLIC_URL", "/jellyfin-widget")
	setEnv(t, "RADARR_URL", "http://radarr.internal:7878")
	setEnv(t, "RADARR_TOKEN", "env-radarr-key")
	setEnv(t, "SONARR_URL", "http://sonarr.internal:8989")
	setEnv(t, "SONARR_TOKEN", "env-sonarr-key")
	setEnv(t, "DOWNLOADING_LIMIT", "25")

	// No jellyfin block in the file at all — env vars alone must supply it.
	path := writeTempConfig(t, `title: ignored`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Jellyfin.URL != "http://jellyfin.internal:8096" {
		t.Errorf("Jellyfin.URL = %q, want env override", cfg.Jellyfin.URL)
	}
	if cfg.Jellyfin.Token != "secret-token" {
		t.Errorf("Jellyfin.Token = %q, want env override", cfg.Jellyfin.Token)
	}
	if cfg.Jellyfin.UserID != "env-user" {
		t.Errorf("Jellyfin.UserID = %q, want env override", cfg.Jellyfin.UserID)
	}
	if cfg.Jellyfin.PublicURL != "https://jf.example.com" {
		t.Errorf("Jellyfin.PublicURL = %q, want env override", cfg.Jellyfin.PublicURL)
	}
	if cfg.Title != "Recently Added" {
		t.Errorf("Title = %q, want env override", cfg.Title)
	}
	if cfg.Limit != 20 {
		t.Errorf("Limit = %d, want 20", cfg.Limit)
	}
	if cfg.PublicURL != "/jellyfin-widget" {
		t.Errorf("PublicURL = %q, want env override", cfg.PublicURL)
	}
}

func TestLoadConfig_MissingRequiredFieldsError(t *testing.T) {
	cases := []struct {
		name   string
		yaml   string
		wantIn string
	}{
		{"missing url", "jellyfin:\n  token: t\n  user_id: u\n  public_url: p\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.url"},
		{"missing token", "jellyfin:\n  url: u\n  user_id: u\n  public_url: p\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.token"},
		{"missing user_id", "jellyfin:\n  url: u\n  token: t\n  public_url: p\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.user_id"},
		{"missing public_url", "jellyfin:\n  url: u\n  token: t\n  user_id: u\nradarr:\n  url: u\n  token: t\nsonarr:\n  url: u\n  token: t", "jellyfin.public_url"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTempConfig(t, c.yaml)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("error = %v, want it to mention %q", err, c.wantIn)
			}
		})
	}
}

func TestLoadConfig_InvalidLimitEnvErrors(t *testing.T) {
	setEnv(t, "LIMIT", "not-a-number")
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid LIMIT, got nil")
	}
}

func TestLoadConfig_RadarrSonarrDefaults(t *testing.T) {
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Radarr.URL != "http://radarr:7878" || cfg.Radarr.Token != "radarr-key" {
		t.Errorf("Radarr = %+v, want {http://radarr:7878 radarr-key}", cfg.Radarr)
	}
	if cfg.Sonarr.URL != "http://sonarr:8989" || cfg.Sonarr.Token != "sonarr-key" {
		t.Errorf("Sonarr = %+v, want {http://sonarr:8989 sonarr-key}", cfg.Sonarr)
	}
	if cfg.DownloadingLimit != 12 {
		t.Errorf("DownloadingLimit = %d, want 12 (default)", cfg.DownloadingLimit)
	}
}

func TestLoadConfig_RadarrSonarrEnvOverrides(t *testing.T) {
	setEnv(t, "JELLYFIN_URL", "http://jellyfin:8096")
	setEnv(t, "JELLYFIN_TOKEN", "t")
	setEnv(t, "JELLYFIN_USER_ID", "u")
	setEnv(t, "JELLYFIN_PUBLIC_URL", "https://jf.example.com")
	setEnv(t, "RADARR_URL", "http://radarr.internal:7878")
	setEnv(t, "RADARR_TOKEN", "env-radarr-key")
	setEnv(t, "SONARR_URL", "http://sonarr.internal:8989")
	setEnv(t, "SONARR_TOKEN", "env-sonarr-key")
	setEnv(t, "DOWNLOADING_LIMIT", "20")

	path := writeTempConfig(t, `title: ignored`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Radarr.URL != "http://radarr.internal:7878" {
		t.Errorf("Radarr.URL = %q, want env override", cfg.Radarr.URL)
	}
	if cfg.Radarr.Token != "env-radarr-key" {
		t.Errorf("Radarr.Token = %q, want env override", cfg.Radarr.Token)
	}
	if cfg.Sonarr.URL != "http://sonarr.internal:8989" {
		t.Errorf("Sonarr.URL = %q, want env override", cfg.Sonarr.URL)
	}
	if cfg.Sonarr.Token != "env-sonarr-key" {
		t.Errorf("Sonarr.Token = %q, want env override", cfg.Sonarr.Token)
	}
	if cfg.DownloadingLimit != 20 {
		t.Errorf("DownloadingLimit = %d, want 20", cfg.DownloadingLimit)
	}
}

func TestLoadConfig_MissingRadarrSonarrFieldsError(t *testing.T) {
	base := "jellyfin:\n  url: u\n  token: t\n  user_id: u\n  public_url: p\n"
	cases := []struct {
		name   string
		yaml   string
		wantIn string
	}{
		{"missing radarr.url", base + "radarr:\n  token: t\nsonarr:\n  url: u\n  token: t", "radarr.url"},
		{"missing radarr.token", base + "radarr:\n  url: u\nsonarr:\n  url: u\n  token: t", "radarr.token"},
		{"missing sonarr.url", base + "radarr:\n  url: u\n  token: t\nsonarr:\n  token: t", "sonarr.url"},
		{"missing sonarr.token", base + "radarr:\n  url: u\n  token: t\nsonarr:\n  url: u", "sonarr.token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTempConfig(t, c.yaml)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("error = %v, want it to mention %q", err, c.wantIn)
			}
		})
	}
}

func TestLoadConfig_InvalidDownloadingLimitEnvErrors(t *testing.T) {
	setEnv(t, "DOWNLOADING_LIMIT", "not-a-number")
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid DOWNLOADING_LIMIT, got nil")
	}
}

func TestLoadConfig_SeerrOptional(t *testing.T) {
	path := writeTempConfig(t, `
jellyfin:
  url: http://jellyfin:8096
  token: test-token
  user_id: test-user
  public_url: https://jellyfin.example.com
radarr:
  url: http://radarr:7878
  token: radarr-key
sonarr:
  url: http://sonarr:8989
  token: sonarr-key
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v (seerr must be optional)", err)
	}
	if cfg.Seerr.PublicURL != "" {
		t.Errorf("Seerr.PublicURL = %q, want empty (no config given)", cfg.Seerr.PublicURL)
	}
}

func TestLoadConfig_SeerrEnvOverride(t *testing.T) {
	setEnv(t, "JELLYFIN_URL", "http://jellyfin:8096")
	setEnv(t, "JELLYFIN_TOKEN", "t")
	setEnv(t, "JELLYFIN_USER_ID", "u")
	setEnv(t, "JELLYFIN_PUBLIC_URL", "https://jf.example.com")
	setEnv(t, "RADARR_URL", "http://radarr:7878")
	setEnv(t, "RADARR_TOKEN", "r")
	setEnv(t, "SONARR_URL", "http://sonarr:8989")
	setEnv(t, "SONARR_TOKEN", "s")
	setEnv(t, "SEERR_PUBLIC_URL", "https://seerr.example.com")

	path := writeTempConfig(t, `title: ignored`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Seerr.PublicURL != "https://seerr.example.com" {
		t.Errorf("Seerr.PublicURL = %q, want env override", cfg.Seerr.PublicURL)
	}
}

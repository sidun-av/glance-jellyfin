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
		{"missing url", "jellyfin:\n  token: t\n  user_id: u\n  public_url: p", "jellyfin.url"},
		{"missing token", "jellyfin:\n  url: u\n  user_id: u\n  public_url: p", "jellyfin.token"},
		{"missing user_id", "jellyfin:\n  url: u\n  token: t\n  public_url: p", "jellyfin.user_id"},
		{"missing public_url", "jellyfin:\n  url: u\n  token: t\n  user_id: u", "jellyfin.public_url"},
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
`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid LIMIT, got nil")
	}
}

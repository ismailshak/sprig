package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_MissingRequiredNamesAll(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	_, err := loadConfig(getenv)
	if err == nil {
		t.Fatal("expected an error for a missing SPRIG_DATABASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "SPRIG_DATABASE_URL") {
		t.Errorf("error %q did not name the missing variable", err.Error())
	}
}

func TestLoadConfig_ReportsEveryProblemAtOnce(t *testing.T) {
	env := map[string]string{
		"SPRIG_LOG_FORMAT": "xml",
		"SPRIG_LOG_LEVEL":  "verbose",
	}
	getenv := func(k string) string { return env[k] }

	_, err := loadConfig(getenv)
	if err == nil {
		t.Fatal("expected an error for three bad settings, got nil")
	}
	for _, want := range []string{"SPRIG_DATABASE_URL", "SPRIG_LOG_FORMAT", "SPRIG_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q did not mention %s", err.Error(), want)
		}
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db"}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if cfg.addr != ":8080" {
		t.Errorf("addr = %q, want %q", cfg.addr, ":8080")
	}
	if cfg.logFormat != "json" {
		t.Errorf("logFormat = %q, want %q", cfg.logFormat, "json")
	}
	if cfg.logLevel != slog.LevelInfo {
		t.Errorf("logLevel = %v, want %v", cfg.logLevel, slog.LevelInfo)
	}
	if cfg.cookie.Name != "__Host-sprig_session" || !cfg.cookie.Secure {
		t.Errorf("cookie = %+v, want __Host-sprig_session and Secure", cfg.cookie)
	}
	if cfg.sessionTTL != 720*time.Hour {
		t.Errorf("sessionTTL = %s, want 720h", cfg.sessionTTL)
	}
}

func TestLoadConfig_APlainCookieOverHTTPTakesBothVariables(t *testing.T) {
	env := map[string]string{
		"SPRIG_DATABASE_URL":  "postgres://example/db",
		"SPRIG_COOKIE_NAME":   "sprig_session",
		"SPRIG_COOKIE_SECURE": "false",
		"SPRIG_SESSION_TTL":   "24h",
	}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if cfg.cookie.Name != "sprig_session" || cfg.cookie.Secure {
		t.Errorf("cookie = %+v, want sprig_session and not Secure", cfg.cookie)
	}
	if cfg.sessionTTL != 24*time.Hour {
		t.Errorf("sessionTTL = %s, want 24h", cfg.sessionTTL)
	}
}

func TestLoadConfig_RejectsACookieABrowserWouldDrop(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"__Host- without Secure", map[string]string{"SPRIG_COOKIE_SECURE": "false"}, "SPRIG_COOKIE_NAME"},
		{"a flag that is not a boolean", map[string]string{"SPRIG_COOKIE_SECURE": "yes"}, "SPRIG_COOKIE_SECURE"},
		{"a TTL that is not a duration", map[string]string{"SPRIG_SESSION_TTL": "30 days"}, "SPRIG_SESSION_TTL"},
		{"a TTL of nothing", map[string]string{"SPRIG_SESSION_TTL": "0s"}, "SPRIG_SESSION_TTL"},
		{"a TTL with a fraction of a second", map[string]string{"SPRIG_SESSION_TTL": "720h0.5s"}, "SPRIG_SESSION_TTL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.env["SPRIG_DATABASE_URL"] = "postgres://example/db"
			getenv := func(k string) string { return c.env[k] }

			_, err := loadConfig(getenv)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q did not name %s", err.Error(), c.want)
			}
		})
	}
}

func TestLoadConfig_OverridesAndTextFormat(t *testing.T) {
	env := map[string]string{
		"SPRIG_DATABASE_URL": "postgres://example/db",
		"SPRIG_ADDR":         ":9090",
		"SPRIG_LOG_FORMAT":   "text",
		"SPRIG_LOG_LEVEL":    "debug",
	}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if cfg.addr != ":9090" {
		t.Errorf("addr = %q, want %q", cfg.addr, ":9090")
	}
	if cfg.logFormat != "text" {
		t.Errorf("logFormat = %q, want %q", cfg.logFormat, "text")
	}
	if cfg.logLevel != slog.LevelDebug {
		t.Errorf("logLevel = %v, want %v", cfg.logLevel, slog.LevelDebug)
	}
}

func TestLoadConfig_RejectsUnknownLogFormat(t *testing.T) {
	env := map[string]string{
		"SPRIG_DATABASE_URL": "postgres://example/db",
		"SPRIG_LOG_FORMAT":   "xml",
	}
	getenv := func(k string) string { return env[k] }

	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("expected an error for an unknown log format, got nil")
	}
}

func TestLoadConfig_RejectsUnknownLogLevel(t *testing.T) {
	env := map[string]string{
		"SPRIG_DATABASE_URL": "postgres://example/db",
		"SPRIG_LOG_LEVEL":    "verbose",
	}
	getenv := func(k string) string { return env[k] }

	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("expected an error for an unknown log level, got nil")
	}
}

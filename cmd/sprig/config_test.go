package main

import (
	"log/slog"
	"strings"
	"testing"
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

package main

import (
	"fmt"
	"log/slog"
	"strings"
)

// config is every setting sprig reads from the environment, gathered once
// at startup.
type config struct {
	addr        string
	databaseURL string
	logLevel    slog.Level
	logFormat   string
}

// loadConfig reads every SPRIG_* variable through getenv. It reports every
// problem it finds at once, so a misconfigured deployment takes one restart
// to diagnose rather than one per variable.
func loadConfig(getenv func(string) string) (config, error) {
	var problems []string
	require := func(name string) string {
		v := getenv(name)
		if v == "" {
			problems = append(problems, name+" is required")
		}
		return v
	}

	cfg := config{
		addr:        withDefault(getenv("SPRIG_ADDR"), ":8080"),
		databaseURL: require("SPRIG_DATABASE_URL"),
		logFormat:   withDefault(getenv("SPRIG_LOG_FORMAT"), "json"),
	}

	if cfg.logFormat != "json" && cfg.logFormat != "text" {
		problems = append(problems, fmt.Sprintf("SPRIG_LOG_FORMAT must be json or text, got %q", cfg.logFormat))
	}

	level, err := parseLogLevel(withDefault(getenv("SPRIG_LOG_LEVEL"), "info"))
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.logLevel = level

	if len(problems) > 0 {
		return config{}, fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}

	return cfg, nil
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseLogLevel(v string) (slog.Level, error) {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("SPRIG_LOG_LEVEL must be one of debug, info, warn, error, got %q", v)
	}
}

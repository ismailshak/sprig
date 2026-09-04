package main

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
)

// config is every setting sprig reads from the environment, gathered once
// at startup.
type config struct {
	addr        string
	databaseURL string
	cookie      auth.CookieSettings
	sessionTTL  time.Duration
	// trustedIPHeader is the header a proxy writes the client address to.
	// Empty means RemoteAddr is the client address.
	trustedIPHeader string
	logLevel        slog.Level
	logFormat       string
}

// defaultSessionTTL is 30 days of disuse before a session ends. A shorter
// window asks for a passkey more often than people tolerate.
const defaultSessionTTL = 720 * time.Hour

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
		addr:            withDefault(getenv("SPRIG_ADDR"), ":8080"),
		databaseURL:     require("SPRIG_DATABASE_URL"),
		trustedIPHeader: strings.TrimSpace(getenv("SPRIG_TRUSTED_IP_HEADER")),
		logFormat:       withDefault(getenv("SPRIG_LOG_FORMAT"), "json"),
	}

	if cfg.logFormat != "json" && cfg.logFormat != "text" {
		problems = append(problems, fmt.Sprintf("SPRIG_LOG_FORMAT must be json or text, got %q", cfg.logFormat))
	}

	cfg.cookie.Name = withDefault(getenv("SPRIG_COOKIE_NAME"), "__Host-sprig_session")
	secure, err := strconv.ParseBool(withDefault(getenv("SPRIG_COOKIE_SECURE"), "true"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("SPRIG_COOKIE_SECURE must be true or false, got %q", getenv("SPRIG_COOKIE_SECURE")))
	}
	cfg.cookie.Secure = secure
	if err == nil {
		if err := cfg.cookie.Validate(); err != nil {
			problems = append(problems, "SPRIG_COOKIE_NAME with SPRIG_COOKIE_SECURE: "+err.Error())
		}
	}

	// Max-Age is an integer, so a TTL with a fraction of a second would give
	// the cookie and the row different deadlines.
	ttl, err := time.ParseDuration(withDefault(getenv("SPRIG_SESSION_TTL"), defaultSessionTTL.String()))
	switch {
	case err != nil:
		problems = append(problems, fmt.Sprintf("SPRIG_SESSION_TTL must be a duration such as 720h, got %q", getenv("SPRIG_SESSION_TTL")))
	case ttl < time.Second || ttl != ttl.Truncate(time.Second):
		problems = append(problems, fmt.Sprintf("SPRIG_SESSION_TTL must be a whole number of seconds and at least one, got %s", ttl))
	}
	cfg.sessionTTL = ttl

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

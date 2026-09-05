package main

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
)

// config is every setting read from the environment, loaded once at startup.
type config struct {
	addr        string
	databaseURL string
	cookie      auth.CookieSettings
	sessionTTL  time.Duration
	// trustedIPHeader is the header a reverse proxy puts the client address
	// in. Empty means RemoteAddr is the client address.
	trustedIPHeader string
	// templateDir is a directory of templates to re-read on every render, for
	// development. Empty means the embedded templates, parsed once.
	templateDir string
	logLevel    slog.Level
	logFormat   string
}

// defaultSessionTTL is 30 days without use before a session expires. Shorter
// would prompt for a passkey more often than people tolerate.
const defaultSessionTTL = 720 * time.Hour

// loadConfig reads every SPRIG_* variable through getenv. It collects every
// problem before returning, so a misconfigured deployment is fixed in one
// restart rather than one per variable.
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
		templateDir:     strings.TrimSpace(getenv("SPRIG_TEMPLATE_DIR")),
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

	// The cookie's Max-Age is whole seconds, so a TTL with a fractional second
	// would give the cookie and the session row different deadlines.
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

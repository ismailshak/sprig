package main

import (
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/push"
)

// config is every setting read from the environment, loaded once at startup.
type config struct {
	addr        string
	databaseURL string
	// baseURL is the origin the app is served at, with no path. A passkey
	// ceremony is checked against it, so it has to be the URL a browser reaches
	// the app at, port included.
	baseURL *url.URL
	// rpID is the WebAuthn relying party id. A passkey is bound to it. It
	// defaults to baseURL's host, and is "localhost" in development.
	rpID       string
	cookie     auth.CookieSettings
	sessionTTL time.Duration
	// trustedIPHeader is the header a reverse proxy puts the client address
	// in. Empty means RemoteAddr is the client address.
	trustedIPHeader string
	// templateDir is a directory of templates to re-read on every render, for
	// development. Empty means the embedded templates, parsed once.
	templateDir string
	photoDir    string
	// photoQuota is the most one garden's photos may add up to, in bytes.
	photoQuota int64
	logLevel   slog.Level
	logFormat  string
	// signupEnabled is whether a stranger can create an account and a garden of
	// their own at /setup. When it is off, the page is served only on an
	// install with no account yet. An invite is then the only other way in.
	signupEnabled bool
	// pushEnabled is whether the app offers notifications at all. With it
	// false the VAPID variables are not read and the Notifications page says
	// notifications are not set up.
	pushEnabled bool
	// push is the VAPID key pair and contact subject. It is empty when
	// pushEnabled is false.
	push push.Keys
}

// defaultAddr is the address the server listens on when SPRIG_ADDR is unset.
// sprig health reads the same variable to find the server.
const defaultAddr = ":8080"

// defaultBaseURL is the address the development server runs on, so a local run
// needs no configuration. A deployment sets SPRIG_BASE_URL, because a browser
// refuses a passkey ceremony whose origin is not the one it is on.
const defaultBaseURL = "http://localhost:8080"

// defaultPhotoQuota is 1 GB counted in decimal bytes. The Garden page reports
// storage in decimal units, so a binary gigabyte would read there as 1.1 GB.
const defaultPhotoQuota = 1_000_000_000

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
		addr:            withDefault(getenv("SPRIG_ADDR"), defaultAddr),
		databaseURL:     require("SPRIG_DATABASE_URL"),
		trustedIPHeader: strings.TrimSpace(getenv("SPRIG_TRUSTED_IP_HEADER")),
		templateDir:     strings.TrimSpace(getenv("SPRIG_TEMPLATE_DIR")),
		photoDir:        withDefault(strings.TrimSpace(getenv("SPRIG_PHOTO_DIR")), "./photos"),
		logFormat:       withDefault(getenv("SPRIG_LOG_FORMAT"), "text"),
	}

	if cfg.logFormat != "json" && cfg.logFormat != "text" {
		problems = append(problems, fmt.Sprintf("SPRIG_LOG_FORMAT must be json or text, got %q", cfg.logFormat))
	}

	base := withDefault(getenv("SPRIG_BASE_URL"), defaultBaseURL)
	baseURL, err := parseBaseURL(base)
	if err != nil {
		problems = append(problems, err.Error())
	}
	cfg.baseURL = baseURL

	// A passkey is bound to the relying party id, so an id the browser will not
	// accept for the page's own origin makes every ceremony fail in the browser
	// with nothing reaching the server. The check is here rather than at the
	// first sign-in attempt.
	if baseURL != nil {
		cfg.rpID = withDefault(strings.TrimSpace(getenv("SPRIG_RP_ID")), baseURL.Hostname())
		if !isRegistrableFor(cfg.rpID, baseURL.Hostname()) {
			problems = append(problems, fmt.Sprintf("SPRIG_RP_ID must be %s or a parent domain of it, got %q", baseURL.Hostname(), cfg.rpID))
		}
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
	// A Secure cookie is the deployed setting and an http base URL is the
	// development default. Together they are a deployment that forgot
	// SPRIG_BASE_URL. It would start, and then every passkey ceremony would be
	// refused, because the browser signs the origin it is on and the server
	// checks that against this one.
	if err == nil && secure && baseURL != nil && baseURL.Scheme != "https" {
		problems = append(problems, fmt.Sprintf("SPRIG_BASE_URL must be https when SPRIG_COOKIE_SECURE is true, got %q", base))
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

	quota, err := parseBytes(withDefault(strings.TrimSpace(getenv("SPRIG_PHOTO_QUOTA")), strconv.Itoa(defaultPhotoQuota)))
	if err != nil {
		problems = append(problems, fmt.Sprintf("SPRIG_PHOTO_QUOTA must be a whole number of bytes above zero, or a number with a unit such as 1GiB, 500MiB or 2GB, got %q", getenv("SPRIG_PHOTO_QUOTA")))
	}
	cfg.photoQuota = quota

	signup, err := strconv.ParseBool(withDefault(getenv("SPRIG_SIGNUP_ENABLED"), "false"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("SPRIG_SIGNUP_ENABLED must be true or false, got %q", getenv("SPRIG_SIGNUP_ENABLED")))
	}
	cfg.signupEnabled = signup

	pushEnabled, err := strconv.ParseBool(withDefault(getenv("SPRIG_PUSH_ENABLED"), "false"))
	if err != nil {
		problems = append(problems, fmt.Sprintf("SPRIG_PUSH_ENABLED must be true or false, got %q", getenv("SPRIG_PUSH_ENABLED")))
	}
	cfg.pushEnabled = pushEnabled
	// The three variables are required only when push is on, so an install
	// without notifications starts without them. A mismatched pair or a
	// subject the push service would refuse is caught here rather than at the
	// first send.
	if pushEnabled {
		cfg.push = push.Keys{
			Public:  strings.TrimSpace(require("SPRIG_VAPID_PUBLIC_KEY")),
			Private: strings.TrimSpace(require("SPRIG_VAPID_PRIVATE_KEY")),
			Subject: strings.TrimSpace(require("SPRIG_VAPID_SUBJECT")),
		}
		if cfg.push.Public != "" && cfg.push.Private != "" && cfg.push.Subject != "" {
			if err := cfg.push.Validate(); err != nil {
				problems = append(problems, "SPRIG_VAPID_PUBLIC_KEY, SPRIG_VAPID_PRIVATE_KEY and SPRIG_VAPID_SUBJECT: "+err.Error())
			}
		}
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

// parseBaseURL returns the scheme and host of a SPRIG_BASE_URL value. Anything
// but an absolute http or https URL with a host is refused, because a passkey
// ceremony is checked against this origin.
func parseBaseURL(v string) (*url.URL, error) {
	parsed, err := url.Parse(v)
	if err != nil {
		return nil, fmt.Errorf("SPRIG_BASE_URL must be an absolute URL such as https://sprig.example.com, got %q", v)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("SPRIG_BASE_URL must be an absolute URL such as https://sprig.example.com, got %q", v)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}

// isRegistrableFor reports whether a browser will accept rpID for a page served
// from host. WebAuthn allows the relying party id to be the host itself or a
// parent domain of it, so a passkey registered on sprig.example.com under the
// id example.com is also offered on app.example.com. A parent domain has to
// contain a dot, because no browser accepts a bare "com" for example.com.
//
// A public suffix of more than one label still passes here. "co.uk" for
// sprig.co.uk is refused by browsers and accepted by this check. Telling those
// apart needs the public suffix list, and the app does not have one. The check
// turns the likely mistake into a message at startup rather than covering
// every wrong value.
func isRegistrableFor(rpID, host string) bool {
	if rpID == host {
		return true
	}
	return strings.Contains(rpID, ".") && strings.HasSuffix(host, "."+rpID)
}

// byteUnits is the multiplier for each unit suffix a byte count accepts. The
// lookup is case-insensitive.
var byteUnits = map[string]int64{
	"":    1,
	"b":   1,
	"kib": 1 << 10,
	"mib": 1 << 20,
	"gib": 1 << 30,
	"tib": 1 << 40,
	"kb":  1e3,
	"mb":  1e6,
	"gb":  1e9,
	"tb":  1e12,
}

// parseBytes reads a count of bytes written as a whole number with an optional
// unit, such as "1073741824", "1GiB" or "2 GB". Zero, a negative number, a
// fraction, an unknown unit and a count past int64 are errors.
func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	suffix := strings.TrimLeftFunc(s, func(r rune) bool { return r >= '0' && r <= '9' })
	unit, ok := byteUnits[strings.ToLower(strings.TrimSpace(suffix))]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q", suffix)
	}
	n, err := strconv.ParseInt(s[:len(s)-len(suffix)], 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("%d is not above zero", n)
	}
	if n > math.MaxInt64/unit {
		return 0, fmt.Errorf("%s is more bytes than can be counted", s)
	}
	return n * unit, nil
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

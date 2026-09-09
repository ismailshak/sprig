package main

import (
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/push"
)

func TestLoadConfig_NamesEveryMissingRequiredVariable(t *testing.T) {
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
	// The base URL is set because the Secure cookie the defaults give needs an
	// https one. Every other value is a default.
	env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db", "SPRIG_BASE_URL": "https://sprig.example.com"}
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
	if cfg.trustedIPHeader != "" {
		t.Errorf("trustedIPHeader = %q, want none, so RemoteAddr is the client until a deployment says otherwise", cfg.trustedIPHeader)
	}
	if cfg.templateDir != "" {
		t.Errorf("templateDir = %q, want none, so the templates are the ones compiled in", cfg.templateDir)
	}
	if cfg.photoDir != "./photos" {
		t.Errorf("photoDir = %q, want ./photos", cfg.photoDir)
	}
	if cfg.photoQuota != 1_000_000_000 {
		t.Errorf("photoQuota = %d, want a decimal gigabyte", cfg.photoQuota)
	}
	if cfg.signupEnabled {
		t.Error("signupEnabled = true, want false, so a stranger cannot make a garden on an install nobody opened up")
	}
}

func TestLoadConfig_SignUpIsOnWhenSPRIG_SIGNUP_ENABLEDIsTrue(t *testing.T) {
	env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db", "SPRIG_BASE_URL": "https://sprig.example.com", "SPRIG_SIGNUP_ENABLED": "true"}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if !cfg.signupEnabled {
		t.Error("signupEnabled = false with SPRIG_SIGNUP_ENABLED=true")
	}
}

func TestLoadConfig_ASignUpFlagThatIsNotABooleanIsRefusedByName(t *testing.T) {
	env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db", "SPRIG_BASE_URL": "https://sprig.example.com", "SPRIG_SIGNUP_ENABLED": "yes"}
	getenv := func(k string) string { return env[k] }

	_, err := loadConfig(getenv)
	if err == nil || !strings.Contains(err.Error(), "SPRIG_SIGNUP_ENABLED") {
		t.Errorf("loadConfig returned %v, want an error naming SPRIG_SIGNUP_ENABLED", err)
	}
}

func TestLoadConfig_APlainCookieOverHTTPNeedsBothCookieVariables(t *testing.T) {
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
		"SPRIG_DATABASE_URL":      "postgres://example/db",
		"SPRIG_BASE_URL":          "https://sprig.example.com",
		"SPRIG_ADDR":              ":9090",
		"SPRIG_LOG_FORMAT":        "text",
		"SPRIG_LOG_LEVEL":         "debug",
		"SPRIG_TRUSTED_IP_HEADER": "CF-Connecting-IP",
		"SPRIG_PHOTO_DIR":         "/srv/photos",
		"SPRIG_PHOTO_QUOTA":       "5368709120",
	}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if cfg.addr != ":9090" {
		t.Errorf("addr = %q, want %q", cfg.addr, ":9090")
	}
	if cfg.photoDir != "/srv/photos" {
		t.Errorf("photoDir = %q, want %q", cfg.photoDir, "/srv/photos")
	}
	if cfg.photoQuota != 5<<30 {
		t.Errorf("photoQuota = %d, want 5 GiB", cfg.photoQuota)
	}
	if cfg.logFormat != "text" {
		t.Errorf("logFormat = %q, want %q", cfg.logFormat, "text")
	}
	if cfg.logLevel != slog.LevelDebug {
		t.Errorf("logLevel = %v, want %v", cfg.logLevel, slog.LevelDebug)
	}
	if cfg.trustedIPHeader != "CF-Connecting-IP" {
		t.Errorf("trustedIPHeader = %q, want CF-Connecting-IP", cfg.trustedIPHeader)
	}
}

func TestLoadConfig_APhotoQuotaTakesAUnitSuffixAndABareNumberIsBytes(t *testing.T) {
	for quota, want := range map[string]int64{
		"1073741824": 1 << 30,
		"1GiB":       1 << 30,
		"500MiB":     500 << 20,
		"2GB":        2_000_000_000,
		"3TiB":       3 << 40,
		"4kb":        4_000,
		"2 GB":       2_000_000_000,
		"12B":        12,
	} {
		env := map[string]string{
			"SPRIG_DATABASE_URL": "postgres://example/db",
			"SPRIG_BASE_URL":     "https://sprig.example.com",
			"SPRIG_PHOTO_QUOTA":  quota,
		}
		getenv := func(k string) string { return env[k] }

		cfg, err := loadConfig(getenv)

		if err != nil {
			t.Errorf("%q: loadConfig returned an error: %v", quota, err)
		} else if cfg.photoQuota != want {
			t.Errorf("%q: photoQuota = %d, want %d", quota, cfg.photoQuota, want)
		}
	}
}

func TestLoadConfig_APhotoQuotaThatIsNotAWholeNumberWithAKnownUnitIsAProblemNamingTheVariable(t *testing.T) {
	for _, quota := range []string{"0", "-1", "0GiB", "1.5", "1.5GiB", "GiB", "1XB", "1GiB extra", "9999999999TiB"} {
		env := map[string]string{
			"SPRIG_DATABASE_URL": "postgres://example/db",
			"SPRIG_BASE_URL":     "https://sprig.example.com",
			"SPRIG_PHOTO_QUOTA":  quota,
		}
		getenv := func(k string) string { return env[k] }

		_, err := loadConfig(getenv)

		if err == nil || !strings.Contains(err.Error(), "SPRIG_PHOTO_QUOTA") {
			t.Errorf("%q: err = %v, want one naming SPRIG_PHOTO_QUOTA", quota, err)
		}
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

func TestLoadConfig_TheRelyingPartyIDDefaultsToTheBaseURLsHost(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no base URL set", nil, "localhost"},
		{"a base URL with a port", map[string]string{"SPRIG_BASE_URL": "http://localhost:9000"}, "localhost"},
		{"a deployed base URL", map[string]string{"SPRIG_BASE_URL": "https://sprig.example.com"}, "sprig.example.com"},
		{
			"a relying party id that is a parent domain of the host",
			map[string]string{"SPRIG_BASE_URL": "https://sprig.example.com", "SPRIG_RP_ID": "example.com"},
			"example.com",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The cookie is the development one, because the default base URL
			// is http and a Secure cookie is refused with it.
			env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db", "SPRIG_COOKIE_NAME": "sprig_session", "SPRIG_COOKIE_SECURE": "false"}
			maps.Copy(env, c.env)
			getenv := func(k string) string { return env[k] }

			cfg, err := loadConfig(getenv)
			if err != nil {
				t.Fatalf("loadConfig returned an error: %v", err)
			}
			if cfg.rpID != c.want {
				t.Errorf("rpID = %q, want %q", cfg.rpID, c.want)
			}
		})
	}
}

func TestLoadConfig_RefusesASetupNoBrowserWouldRunAPasskeyCeremonyUnder(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"a base URL with no scheme", map[string]string{"SPRIG_BASE_URL": "sprig.example.com"}, "SPRIG_BASE_URL"},
		{"a base URL with no host", map[string]string{"SPRIG_BASE_URL": "https://"}, "SPRIG_BASE_URL"},
		{
			"a relying party id that is not a parent domain of the host",
			map[string]string{"SPRIG_BASE_URL": "https://sprig.example.com", "SPRIG_RP_ID": "example.org"},
			"SPRIG_RP_ID",
		},
		{
			"a relying party id that is a single label the host ends with",
			map[string]string{"SPRIG_BASE_URL": "https://sprig.example.com", "SPRIG_RP_ID": "com"},
			"SPRIG_RP_ID",
		},
		{
			"a relying party id that only shares a suffix with the host",
			map[string]string{"SPRIG_BASE_URL": "https://notexample.com", "SPRIG_RP_ID": "example.com"},
			"SPRIG_RP_ID",
		},
		// A Secure cookie with an http base URL is a deployment that forgot to
		// set SPRIG_BASE_URL. The server would start and refuse every ceremony.
		{"the default base URL with the default Secure cookie", map[string]string{}, "SPRIG_BASE_URL"},
		{
			"an http base URL with a Secure cookie",
			map[string]string{"SPRIG_BASE_URL": "http://sprig.example.com", "SPRIG_COOKIE_SECURE": "true"},
			"SPRIG_BASE_URL",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.env["SPRIG_DATABASE_URL"] = "postgres://example/db"
			getenv := func(k string) string { return c.env[k] }

			_, err := loadConfig(getenv)
			if err == nil {
				t.Fatal("loadConfig accepted it, want an error naming " + c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to name %s", err, c.want)
			}
		})
	}
}

func TestLoadConfig_PushIsOffByDefaultAndNeedsNoKeys(t *testing.T) {
	env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db", "SPRIG_BASE_URL": "https://sprig.example.com"}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if cfg.pushEnabled {
		t.Error("pushEnabled = true, want false, so a laptop does not subscribe to a real push service unless asked")
	}
}

func TestLoadConfig_PushOnNamesEveryMissingVAPIDVariable(t *testing.T) {
	env := map[string]string{"SPRIG_DATABASE_URL": "postgres://example/db", "SPRIG_BASE_URL": "https://sprig.example.com", "SPRIG_PUSH_ENABLED": "true"}
	getenv := func(k string) string { return env[k] }

	_, err := loadConfig(getenv)
	if err == nil {
		t.Fatal("expected an error for push on with no keys, got nil")
	}
	for _, want := range []string{"SPRIG_VAPID_PUBLIC_KEY", "SPRIG_VAPID_PRIVATE_KEY", "SPRIG_VAPID_SUBJECT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q did not name %s", err.Error(), want)
		}
	}
}

func TestLoadConfig_PushOnReadsTheKeysAndSubject(t *testing.T) {
	keys, err := push.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	env := map[string]string{
		"SPRIG_DATABASE_URL":      "postgres://example/db",
		"SPRIG_BASE_URL":          "https://sprig.example.com",
		"SPRIG_PUSH_ENABLED":      "true",
		"SPRIG_VAPID_PUBLIC_KEY":  keys.Public,
		"SPRIG_VAPID_PRIVATE_KEY": keys.Private,
		"SPRIG_VAPID_SUBJECT":     "mailto:sprig@example.com",
	}
	getenv := func(k string) string { return env[k] }

	cfg, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if !cfg.pushEnabled {
		t.Error("pushEnabled = false, want true")
	}
	if cfg.push.Public != keys.Public || cfg.push.Private != keys.Private || cfg.push.Subject != "mailto:sprig@example.com" {
		t.Errorf("push = %+v, want the keys and subject the environment gave", cfg.push)
	}
}

func TestLoadConfig_APublicKeyFromAnotherPairIsRefusedAtStartup(t *testing.T) {
	first, _ := push.GenerateKeys()
	second, _ := push.GenerateKeys()
	env := map[string]string{
		"SPRIG_DATABASE_URL":      "postgres://example/db",
		"SPRIG_BASE_URL":          "https://sprig.example.com",
		"SPRIG_PUSH_ENABLED":      "true",
		"SPRIG_VAPID_PUBLIC_KEY":  first.Public,
		"SPRIG_VAPID_PRIVATE_KEY": second.Private,
		"SPRIG_VAPID_SUBJECT":     "mailto:sprig@example.com",
	}
	getenv := func(k string) string { return env[k] }

	_, err := loadConfig(getenv)

	if err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("a mismatched pair started with %v, want a startup error naming the mismatch", err)
	}
}

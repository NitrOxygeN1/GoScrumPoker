package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is loaded from environment variables with local-dev-friendly defaults.
//
// Core:
//   - PORT — HTTP listen address port (e.g. "10000", "8080"). Render and most PaaS set this — bind with ":"+PORT (all interfaces). Default 8080.
//   - DATABASE_URL — Postgres (libpq/Render-compatible URL; pgx connections use sslmode=require).
//   - REDIS_URL — optional redis:// or rediss:// for room storage.
//   - ENV — "dev" or "prod". If unset and RENDER is set (Render.com), defaults to prod.
//   - RENDER — set by render.com; used to pick prod defaults and logging.
//   - WEB_ROOT — if set, directory with Vite `dist` output (index.html + assets/) to serve the SPA
//     at / and for HTML5 fallback. Docker sets this to /app/web. Leave empty to serve API only.
//
//   - RUN_MIGRATIONS_ON_STARTUP — when "true" runs SQL migrate "up" before serving; when
//     unset, defaults to true for prod or Render when DATABASE_URL is set (so deployed Postgres
//     always gets schema). Set to "false" to skip (e.g. migrations run in CI only).
//
// Additional (optional) env vars are still read in Load for auth, migrations, and Redis TTL.
type Config struct {
	Port         string
	DatabaseURL  string
	RedisURL     string
	Env          string
	WebRoot      string

	ShutdownTimeout        time.Duration
	MigrationsPath         string
	RunMigrationsOnStartup bool
	Auth                   AuthConfig
	RedisRoomTTL           time.Duration
	// MeetIFrameEmbed sets SameSite=None; Secure on auth cookies and is on by default (Google Meet iframe).
	MeetIFrameEmbed bool
	// CSPFrameAncestorsExtra adds optional frame-ancestors sources (space-separated host sources).
	CSPFrameAncestorsExtra string
	// GoogleCloudProjectNumber is the Google Cloud project number (NOT project id) used to
	// initialize the Meet Web Add-ons SDK in the SPA. When set, the server injects it into
	// index.html as <meta name="gsp-cloud-project-number">. Without it the SPA still runs
	// standalone, but Meet's host shell will show "Failed to launch the add-on".
	GoogleCloudProjectNumber string
}

// AuthConfig holds Google OAuth2 and session settings (from env).
type AuthConfig struct {
	GoogleClientID     string
	GoogleClientSecret string
	OAuthRedirectURL   string
	JWTSecret          string
	JWTExpires         time.Duration
	PostLoginRedirect  string
	CookieSecure       bool
}

// IsRender returns true when running on Render (RENDER=1|true|yes, set by the platform).
func IsRender() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("RENDER")))
	return v == "1" || v == "true" || v == "yes"
}

// ExposeHealthErrorDetail reports whether /health may include raw database error text (dev only; never on Render in prod).
func (c Config) ExposeHealthErrorDetail() bool {
	if IsRender() {
		return false
	}
	return c.Env == "dev"
}

// Load reads configuration from the environment.
func Load() Config {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV")))
	if env == "" {
		if IsRender() {
			env = "prod"
		} else {
			env = "dev"
		}
	}

	// Graceful shutdown budget (HTTP + WebSockets); clamped to 5–10s.
	timeout := 8 * time.Second
	if v := os.Getenv("SHUTDOWN_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}

	jwtHours := 168
	if v := os.Getenv("JWT_EXPI_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			jwtHours = n
		}
	}

	isProd := env == "prod"
	cookieSecure := isProd
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SECURE"))); v == "false" || v == "0" {
		cookieSecure = false
	} else if truthy(os.Getenv("COOKIE_SECURE")) {
		cookieSecure = true
	}

	roomTTL := 24 * time.Hour
	if v := os.Getenv("ROOM_INACTIVE_TTL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			roomTTL = time.Duration(n) * time.Second
		}
	}

	migrationsPath := firstNonEmpty(os.Getenv("MIGRATIONS_PATH"), "migrations")

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	mRunV, mRunSet := os.LookupEnv("RUN_MIGRATIONS_ON_STARTUP")
	runMigrations := resolveRunMigrationsOnStartup(mRunSet, mRunV, databaseURL, isProd)

	meetEmbed := true
	if v, ok := os.LookupEnv("MEET_IFRAME_EMBED"); ok {
		meetEmbed = truthy(v)
	}

	return Config{
		Port:                   port,
		DatabaseURL:            databaseURL,
		RedisURL:               strings.TrimSpace(os.Getenv("REDIS_URL")),
		Env:                    env,
		WebRoot:                strings.TrimSpace(os.Getenv("WEB_ROOT")),
		ShutdownTimeout:        timeout,
		MigrationsPath:         migrationsPath,
		RunMigrationsOnStartup: runMigrations,
		RedisRoomTTL:           roomTTL,
		MeetIFrameEmbed:          meetEmbed,
		CSPFrameAncestorsExtra:   strings.TrimSpace(os.Getenv("CSP_FRAME_ANCESTORS_EXTRA")),
		GoogleCloudProjectNumber: strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT_NUMBER")),
		Auth: AuthConfig{
			GoogleClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
			GoogleClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
			OAuthRedirectURL:   os.Getenv("OAUTH_REDIRECT_URL"),
			JWTSecret:          os.Getenv("JWT_SECRET"),
			JWTExpires:         time.Duration(jwtHours) * time.Hour,
			PostLoginRedirect:  firstNonEmpty(os.Getenv("POST_LOGIN_REDIRECT"), "/"),
			CookieSecure:       cookieSecure,
		},
	}
}

// ListenAddr returns a value suitable for http.Server.Addr.
func (c Config) ListenAddr() string {
	p := strings.TrimSpace(c.Port)
	if p == "" {
		return ":8080"
	}
	if strings.HasPrefix(p, ":") {
		return p
	}
	if strings.Contains(p, ":") {
		return p
	}
	return ":" + p
}

// IsProd reports whether ENV is production.
func (c Config) IsProd() bool {
	return c.Env == "prod"
}

func resolveRunMigrationsOnStartup(explicitlySet bool, v string, databaseURL string, isProd bool) bool {
	if explicitlySet {
		if strings.TrimSpace(v) == "" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "no", "off":
			return false
		default:
			return truthy(v)
		}
	}
	if databaseURL == "" {
		return false
	}
	return isProd || IsRender()
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

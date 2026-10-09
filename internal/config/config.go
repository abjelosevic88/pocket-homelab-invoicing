// Package config loads runtime configuration from environment variables.
//
// Every option has a sensible default so that `docker run` with a single
// volume works out of the box. See docs/configuration.md for the full list.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	// Server
	Host           string
	Port           int
	BaseURL        string // public URL, used in emails and public invoice links
	DataDir        string
	LogLevel       string
	LogFormat      string // "text" or "json"
	TrustProxy     bool   // honour X-Forwarded-* headers
	MetricsEnabled bool

	// Database
	DBPath string

	// Auth
	AuthMode             string // "local" or "proxy"
	AuthProxyHeader      string // header carrying the username when AuthMode == proxy
	AuthProxyEmailHeader string
	SessionSecret        string
	SessionTTL           time.Duration
	AdminEmail           string // bootstrap admin (optional)
	AdminPassword        string
	AdminName            string
	SecureCookies        bool

	// PDF
	PDFEngine       string // native | chromium | gotenberg
	ChromiumPath    string
	GotenbergURL    string
	DocxConverter   string // gotenberg | libreoffice | none
	LibreOfficePath string

	// Currency
	ExchangeRateProvider string // frankfurter | none
	ExchangeRateRefresh  time.Duration

	// Scheduler
	SchedulerEnabled  bool
	SchedulerInterval time.Duration

	// Paperless-ngx (optional defaults; Settings → Paperless overrides)
	PaperlessURL   string
	PaperlessToken string

	// Misc
	DemoData bool
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(env(key, ""))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := env(key, ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	dataDir := env("DATA_DIR", "./data")
	c := &Config{
		Host:           env("HOST", "0.0.0.0"),
		Port:           envInt("PORT", 8080),
		BaseURL:        strings.TrimRight(env("BASE_URL", ""), "/"),
		DataDir:        dataDir,
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:      strings.ToLower(env("LOG_FORMAT", "text")),
		TrustProxy:     envBool("TRUST_PROXY", true),
		MetricsEnabled: envBool("METRICS_ENABLED", true),

		DBPath: env("DB_PATH", dataDir+"/pocket-invoicing.db"),

		AuthMode:             strings.ToLower(env("AUTH_MODE", "local")),
		AuthProxyHeader:      env("AUTH_PROXY_HEADER", "Remote-User"),
		AuthProxyEmailHeader: env("AUTH_PROXY_EMAIL_HEADER", "Remote-Email"),
		SessionSecret:        env("SESSION_SECRET", ""),
		SessionTTL:           envDuration("SESSION_TTL", 720*time.Hour),
		AdminEmail:           env("ADMIN_EMAIL", ""),
		AdminPassword:        env("ADMIN_PASSWORD", ""),
		AdminName:            env("ADMIN_NAME", "Admin"),
		SecureCookies:        envBool("SECURE_COOKIES", false),

		PDFEngine:       strings.ToLower(env("PDF_ENGINE", "native")),
		ChromiumPath:    env("CHROMIUM_PATH", "chromium"),
		GotenbergURL:    strings.TrimRight(env("GOTENBERG_URL", "http://gotenberg:3000"), "/"),
		DocxConverter:   strings.ToLower(env("DOCX_CONVERTER", "gotenberg")),
		PaperlessURL:    strings.TrimRight(env("PAPERLESS_URL", ""), "/"),
		PaperlessToken:  env("PAPERLESS_TOKEN", ""),
		LibreOfficePath: env("LIBREOFFICE_PATH", "soffice"),

		ExchangeRateProvider: strings.ToLower(env("EXCHANGE_RATE_PROVIDER", "frankfurter")),
		ExchangeRateRefresh:  envDuration("EXCHANGE_RATE_REFRESH", 12*time.Hour),

		SchedulerEnabled:  envBool("SCHEDULER_ENABLED", true),
		SchedulerInterval: envDuration("SCHEDULER_INTERVAL", 15*time.Minute),

		DemoData: envBool("DEMO_DATA", false),
	}

	switch c.AuthMode {
	case "local", "proxy", "none":
	default:
		return nil, fmt.Errorf("AUTH_MODE must be one of local, proxy, none (got %q)", c.AuthMode)
	}
	switch c.DocxConverter {
	case "gotenberg", "libreoffice", "soffice", "none":
	default:
		return nil, fmt.Errorf("DOCX_CONVERTER must be one of gotenberg, libreoffice, none (got %q)", c.DocxConverter)
	}
	switch c.PDFEngine {
	case "native", "chromium", "gotenberg":
	default:
		return nil, fmt.Errorf("PDF_ENGINE must be one of native, chromium, gotenberg (got %q)", c.PDFEngine)
	}
	if c.BaseURL == "" {
		c.BaseURL = fmt.Sprintf("http://localhost:%d", c.Port)
	}
	return c, nil
}

// Addr returns the listen address.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

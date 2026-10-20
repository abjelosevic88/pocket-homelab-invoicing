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

	// Sign in with an OpenID Connect provider. On a box with Pocket Core none
	// of these are needed: the app uses the client Core issues on registration.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCName         string // button label
	OIDCAutoCreate   bool   // unknown emails get an account

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

	// Pocket Documents: where a direct connection reaches it when the person
	// gave no URL and Pocket Core cannot say (Settings → Pocket Documents).
	// Tokens are personal (pd_…), so there is no box-wide one.
	DocumentsURL string
	// PaperlessEnvSet: PAPERLESS_URL / PAPERLESS_TOKEN are still set; they are
	// ignored now that Pocket Documents replaced Paperless (logged once at startup).
	PaperlessEnvSet bool

	// Pocket Core: when CORE_URL is set the app registers itself on start,
	// publishes events and accepts the tokens Core hands to paired phones.
	CoreURL             string
	CoreRegistrationKey string
	CoreAppURL          string // how Core reaches this app; defaults to BASE_URL

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
		OIDCIssuer:           strings.TrimRight(env("OIDC_ISSUER", ""), "/"),
		OIDCClientID:         env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:     env("OIDC_CLIENT_SECRET", ""),
		OIDCName:             env("OIDC_NAME", "single sign-on"),
		OIDCAutoCreate:       envBool("OIDC_AUTO_CREATE", true),

		PDFEngine:       strings.ToLower(env("PDF_ENGINE", "native")),
		ChromiumPath:    env("CHROMIUM_PATH", "chromium"),
		GotenbergURL:    strings.TrimRight(env("GOTENBERG_URL", "http://gotenberg:3000"), "/"),
		DocxConverter:   strings.ToLower(env("DOCX_CONVERTER", "gotenberg")),
		DocumentsURL:    strings.TrimRight(env("DOCUMENTS_URL", ""), "/"),
		PaperlessEnvSet: env("PAPERLESS_URL", "") != "" || env("PAPERLESS_TOKEN", "") != "",
		LibreOfficePath: env("LIBREOFFICE_PATH", "soffice"),

		ExchangeRateProvider: strings.ToLower(env("EXCHANGE_RATE_PROVIDER", "frankfurter")),
		ExchangeRateRefresh:  envDuration("EXCHANGE_RATE_REFRESH", 12*time.Hour),

		SchedulerEnabled:  envBool("SCHEDULER_ENABLED", true),
		SchedulerInterval: envDuration("SCHEDULER_INTERVAL", 15*time.Minute),

		CoreURL:             strings.TrimRight(env("CORE_URL", ""), "/"),
		CoreRegistrationKey: env("CORE_REGISTRATION_KEY", ""),
		CoreAppURL:          strings.TrimRight(env("CORE_APP_URL", ""), "/"),

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
	if c.OIDCIssuer != "" && (c.OIDCClientID == "" || c.BaseURL == "") {
		return nil, fmt.Errorf("OIDC_ISSUER needs OIDC_CLIENT_ID and BASE_URL (the callback is BASE_URL/api/v1/auth/oidc/callback)")
	}
	if c.BaseURL == "" {
		c.BaseURL = fmt.Sprintf("http://localhost:%d", c.Port)
	}
	if c.CoreAppURL == "" {
		c.CoreAppURL = c.BaseURL
	}
	return c, nil
}

// Addr returns the listen address.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

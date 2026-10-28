// Package config loads backend settings from environment variables.
package config

import (
	"errors"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAppPort      = 8080
	defaultInternalPort = 8081
	defaultAIServiceURL = "http://127.0.0.1:8090"
	defaultUserLimit    = 30
	defaultSiteLimit    = 300
	defaultSSLMode      = "disable"
	defaultJWTTTL       = 7 * 24 * time.Hour
	defaultJWTIssuer    = "mogumogu"
	defaultJWTAudience  = "mogumogu-web"
	minSecretBytes      = 32
)

var validSSLModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

type Config struct {
	AppPort int
	// InternalPort serves the API the AI service's query tools call.
	InternalPort int
	DB           DBConfig
	Auth         AuthConfig
	Assistant    AssistantConfig
	// TrustedProxies lists the IPs or CIDR ranges whose X-Forwarded-For header
	// is believed. Empty (the default) means the TCP peer is the client.
	TrustedProxies []string
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type AuthConfig struct {
	JWTSecret      []byte
	JWTTTL         time.Duration
	JWTIssuer      string
	JWTAudience    string
	CSRFSecret     []byte
	AllowedOrigins []string
	CookieSecure   bool
}

type AssistantConfig struct {
	// ServiceURL is the base URL of the AI service (ai/).
	ServiceURL string
	// Daily message quota per user and for the whole site.
	UserLimit int
	SiteLimit int
}

// LogValue keeps the password out of logs if the struct is ever logged.
func (c DBConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("host", c.Host),
		slog.Int("port", c.Port),
		slog.String("user", c.User),
		slog.String("name", c.Name),
		slog.String("sslmode", c.SSLMode),
	)
}

// LogValue keeps both secrets out of logs if the struct is ever logged.
func (c AuthConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Duration("jwt_ttl", c.JWTTTL),
		slog.String("jwt_issuer", c.JWTIssuer),
		slog.String("jwt_audience", c.JWTAudience),
		slog.Any("allowed_origins", c.AllowedOrigins),
		slog.Bool("cookie_secure", c.CookieSecure),
	)
}

// FromEnv loads the full server configuration from the process environment.
func FromEnv() (Config, error) {
	return Load(os.LookupEnv)
}

// DBFromEnv loads only the database settings, for tools such as migrate.
func DBFromEnv() (DBConfig, error) {
	return LoadDB(os.LookupEnv)
}

// Load builds a Config using lookup to read variables. All problems are
// reported together. Error messages name variables but never include values.
func Load(lookup func(string) (string, bool)) (Config, error) {
	r := newReader(lookup)
	cfg := Config{
		AppPort:        r.port("APP_PORT", false, DefaultAppPort),
		InternalPort:   r.port("INTERNAL_PORT", false, defaultInternalPort),
		DB:             r.db(),
		Auth:           r.auth(),
		Assistant:      r.assistant(),
		TrustedProxies: r.trustedProxies(),
	}
	if cfg.AppPort != 0 && cfg.AppPort == cfg.InternalPort {
		return Config{}, errors.New("invalid configuration: INTERNAL_PORT must differ from APP_PORT")
	}
	if err := r.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadDB is Load restricted to the DB_* variables.
func LoadDB(lookup func(string) (string, bool)) (DBConfig, error) {
	r := newReader(lookup)
	db := r.db()
	if err := r.err(); err != nil {
		return DBConfig{}, err
	}
	return db, nil
}

type reader struct {
	lookup   func(string) (string, bool)
	missing  []string
	problems []string
}

func newReader(lookup func(string) (string, bool)) *reader {
	return &reader{lookup: lookup}
}

func (r *reader) get(key string) string {
	v, _ := r.lookup(key)
	return strings.TrimSpace(v)
}

func (r *reader) required(key string) string {
	v := r.get(key)
	if v == "" {
		r.missing = append(r.missing, key)
	}
	return v
}

func (r *reader) port(key string, required bool, def int) int {
	raw := r.get(key)
	if raw == "" {
		if required {
			r.missing = append(r.missing, key)
		}
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		r.problems = append(r.problems, key+" must be an integer between 1 and 65535")
		return 0
	}
	return n
}

func (r *reader) db() DBConfig {
	db := DBConfig{
		Host:     r.required("DB_HOST"),
		Port:     r.port("DB_PORT", true, 0),
		User:     r.required("DB_USER"),
		Password: r.required("DB_PASSWORD"),
		Name:     r.required("DB_NAME"),
		SSLMode:  defaultSSLMode,
	}
	if mode := r.get("DB_SSLMODE"); mode != "" {
		db.SSLMode = mode
		if !slices.Contains(validSSLModes, mode) {
			r.problems = append(r.problems, "DB_SSLMODE must be one of: "+strings.Join(validSSLModes, ", "))
		}
	}
	return db
}

func (r *reader) auth() AuthConfig {
	a := AuthConfig{
		JWTSecret:   r.secret("JWT_SECRET"),
		JWTTTL:      defaultJWTTTL,
		JWTIssuer:   defaultJWTIssuer,
		JWTAudience: defaultJWTAudience,
		CSRFSecret:  r.secret("CSRF_SECRET"),
	}
	if a.JWTSecret != nil && a.CSRFSecret != nil && string(a.JWTSecret) == string(a.CSRFSecret) {
		r.problems = append(r.problems, "CSRF_SECRET must differ from JWT_SECRET")
	}
	if raw := r.get("JWT_TTL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < time.Minute {
			r.problems = append(r.problems, "JWT_TTL must be a duration of at least 1m, such as 168h")
		} else {
			a.JWTTTL = d
		}
	}
	if v := r.get("JWT_ISSUER"); v != "" {
		a.JWTIssuer = v
	}
	if v := r.get("JWT_AUDIENCE"); v != "" {
		a.JWTAudience = v
	}
	if raw := r.required("ALLOWED_ORIGINS"); raw != "" {
		for o := range strings.SplitSeq(raw, ",") {
			o = strings.TrimSpace(o)
			if !isOrigin(o) {
				r.problems = append(r.problems, "ALLOWED_ORIGINS must be a comma-separated list of origins such as http://localhost:5173")
				break
			}
			a.AllowedOrigins = append(a.AllowedOrigins, o)
		}
	}
	if raw := r.get("COOKIE_SECURE"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			r.problems = append(r.problems, "COOKIE_SECURE must be true or false")
		}
		a.CookieSecure = b
	}
	return a
}

func (r *reader) assistant() AssistantConfig {
	a := AssistantConfig{
		ServiceURL: defaultAIServiceURL,
		UserLimit:  r.positiveInt("ASSISTANT_USER_DAILY_LIMIT", defaultUserLimit),
		SiteLimit:  r.positiveInt("ASSISTANT_SITE_DAILY_LIMIT", defaultSiteLimit),
	}
	if raw := r.get("AI_SERVICE_URL"); raw != "" {
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			r.problems = append(r.problems, "AI_SERVICE_URL must be a URL such as http://127.0.0.1:8090")
		} else {
			a.ServiceURL = strings.TrimSuffix(raw, "/")
		}
	}
	return a
}

func (r *reader) positiveInt(key string, def int) int {
	raw := r.get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		r.problems = append(r.problems, key+" must be a positive integer")
		return def
	}
	return n
}

// trustedProxies reads TRUSTED_PROXIES, a comma-separated list of IPs or CIDR
// ranges such as 172.16.0.0/12. Only set it when the backend is reachable
// solely through a proxy that overwrites X-Forwarded-For.
func (r *reader) trustedProxies() []string {
	raw := r.get("TRUSTED_PROXIES")
	if raw == "" {
		return nil
	}
	var out []string
	for p := range strings.SplitSeq(raw, ",") {
		p = strings.TrimSpace(p)
		if _, err := netip.ParsePrefix(p); err != nil {
			if _, err := netip.ParseAddr(p); err != nil {
				r.problems = append(r.problems, "TRUSTED_PROXIES must be a comma-separated list of IPs or CIDR ranges such as 172.16.0.0/12")
				return nil
			}
		}
		out = append(out, p)
	}
	return out
}

func (r *reader) secret(key string) []byte {
	v := r.required(key)
	if v == "" {
		return nil
	}
	if len(v) < minSecretBytes {
		r.problems = append(r.problems, key+" must be at least 32 bytes")
		return nil
	}
	return []byte(v)
}

func (r *reader) err() error {
	problems := r.problems
	if len(r.missing) > 0 {
		problems = append([]string{"missing required environment variables: " + strings.Join(r.missing, ", ")}, problems...)
	}
	if len(problems) > 0 {
		return errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return nil
}

// isOrigin accepts scheme://host[:port] with no path, query or trailing slash,
// which is exactly what browsers send in the Origin header.
func isOrigin(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

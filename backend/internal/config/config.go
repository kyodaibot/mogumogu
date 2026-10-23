// Package config loads backend settings from environment variables.
package config

import (
	"errors"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultAppPort = 8080
	defaultSSLMode = "disable"
)

var validSSLModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

type Config struct {
	AppPort int
	DB      DBConfig
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
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

// FromEnv loads the configuration from the process environment.
func FromEnv() (Config, error) {
	return Load(os.LookupEnv)
}

// Load builds a Config using lookup to read variables. All problems are
// reported together. Error messages name variables but never include values.
func Load(lookup func(string) (string, bool)) (Config, error) {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	var missing []string
	var problems []string

	required := func(key string) string {
		v := get(key)
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	port := func(key, raw string) int {
		if raw == "" {
			return 0
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 65535 {
			problems = append(problems, key+" must be an integer between 1 and 65535")
			return 0
		}
		return n
	}

	cfg := Config{
		AppPort: defaultAppPort,
		DB: DBConfig{
			Host:     required("DB_HOST"),
			User:     required("DB_USER"),
			Password: required("DB_PASSWORD"),
			Name:     required("DB_NAME"),
			SSLMode:  defaultSSLMode,
		},
	}
	cfg.DB.Port = port("DB_PORT", required("DB_PORT"))
	if raw := get("APP_PORT"); raw != "" {
		cfg.AppPort = port("APP_PORT", raw)
	}
	if mode := get("DB_SSLMODE"); mode != "" {
		cfg.DB.SSLMode = mode
		if !slices.Contains(validSSLModes, mode) {
			problems = append(problems, "DB_SSLMODE must be one of: "+strings.Join(validSSLModes, ", "))
		}
	}

	if len(missing) > 0 {
		problems = append([]string{"missing required environment variables: " + strings.Join(missing, ", ")}, problems...)
	}
	if len(problems) > 0 {
		return Config{}, errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return cfg, nil
}

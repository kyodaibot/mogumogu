package config

import (
	"strings"
	"testing"
	"time"
)

// Obviously fake, low-entropy values that satisfy the 32-byte minimum.
const testPassword = "test-placeholder-pw"

var (
	testJWTSecret  = strings.Repeat("j", 40)
	testCSRFSecret = strings.Repeat("c", 40)
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"DB_HOST":         "localhost",
		"DB_PORT":         "5432",
		"DB_USER":         "app",
		"DB_PASSWORD":     testPassword,
		"DB_NAME":         "appdb",
		"JWT_SECRET":      testJWTSecret,
		"CSRF_SECRET":     testCSRFSecret,
		"ALLOWED_ORIGINS": "http://localhost:5173",
	}
}

func TestLoadValidWithDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDB := DBConfig{
		Host: "localhost", Port: 5432, User: "app",
		Password: testPassword, Name: "appdb", SSLMode: "disable",
	}
	if cfg.AppPort != 8080 || cfg.DB != wantDB {
		t.Fatalf("got AppPort=%d DB=%+v", cfg.AppPort, cfg.DB)
	}
	a := cfg.Auth
	if string(a.JWTSecret) != testJWTSecret || string(a.CSRFSecret) != testCSRFSecret {
		t.Fatal("secrets not loaded")
	}
	if a.JWTTTL != 7*24*time.Hour || a.JWTIssuer != "mogumogu" || a.JWTAudience != "mogumogu-web" || a.CookieSecure {
		t.Fatalf("unexpected auth defaults: %+v", a.LogValue())
	}
	if len(a.AllowedOrigins) != 1 || a.AllowedOrigins[0] != "http://localhost:5173" {
		t.Fatalf("allowed origins = %v", a.AllowedOrigins)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	env := validEnv()
	env["APP_PORT"] = "9090"
	env["DB_SSLMODE"] = "require"
	env["JWT_TTL"] = "24h"
	env["JWT_ISSUER"] = "iss"
	env["JWT_AUDIENCE"] = "aud"
	env["ALLOWED_ORIGINS"] = "http://localhost:5173, https://app.example.com"
	env["COOKIE_SECURE"] = "true"

	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := cfg.Auth
	if cfg.AppPort != 9090 || cfg.DB.SSLMode != "require" || a.JWTTTL != 24*time.Hour ||
		a.JWTIssuer != "iss" || a.JWTAudience != "aud" || !a.CookieSecure ||
		len(a.AllowedOrigins) != 2 || a.AllowedOrigins[1] != "https://app.example.com" {
		t.Fatalf("overrides not applied: port=%d db=%+v auth=%+v", cfg.AppPort, cfg.DB.LogValue(), a.LogValue())
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(env map[string]string)
		wantErr []string
	}{
		{
			name:    "missing one",
			modify:  func(env map[string]string) { delete(env, "DB_HOST") },
			wantErr: []string{"missing required environment variables: DB_HOST"},
		},
		{
			name: "missing several reported together",
			modify: func(env map[string]string) {
				delete(env, "DB_USER")
				delete(env, "JWT_SECRET")
				delete(env, "ALLOWED_ORIGINS")
			},
			wantErr: []string{"DB_USER", "JWT_SECRET", "ALLOWED_ORIGINS"},
		},
		{
			name:    "blank counts as missing",
			modify:  func(env map[string]string) { env["DB_PASSWORD"] = "   " },
			wantErr: []string{"missing required environment variables: DB_PASSWORD"},
		},
		{
			name:    "invalid db port",
			modify:  func(env map[string]string) { env["DB_PORT"] = "abc" },
			wantErr: []string{"DB_PORT must be an integer"},
		},
		{
			name:    "app port out of range",
			modify:  func(env map[string]string) { env["APP_PORT"] = "70000" },
			wantErr: []string{"APP_PORT must be an integer"},
		},
		{
			name:    "invalid sslmode",
			modify:  func(env map[string]string) { env["DB_SSLMODE"] = "maybe" },
			wantErr: []string{"DB_SSLMODE must be one of"},
		},
		{
			name:    "short jwt secret",
			modify:  func(env map[string]string) { env["JWT_SECRET"] = "too-short" },
			wantErr: []string{"JWT_SECRET must be at least 32 bytes"},
		},
		{
			name:    "csrf secret reuses jwt secret",
			modify:  func(env map[string]string) { env["CSRF_SECRET"] = testJWTSecret },
			wantErr: []string{"CSRF_SECRET must differ from JWT_SECRET"},
		},
		{
			name:    "invalid jwt ttl",
			modify:  func(env map[string]string) { env["JWT_TTL"] = "7d" },
			wantErr: []string{"JWT_TTL must be a duration"},
		},
		{
			name:    "origin with path",
			modify:  func(env map[string]string) { env["ALLOWED_ORIGINS"] = "http://localhost:5173/" },
			wantErr: []string{"ALLOWED_ORIGINS must be"},
		},
		{
			name:    "invalid cookie secure",
			modify:  func(env map[string]string) { env["COOKIE_SECURE"] = "yes please" },
			wantErr: []string{"COOKIE_SECURE must be true or false"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			tt.modify(env)

			_, err := Load(lookupFrom(env))
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error()
			for _, want := range tt.wantErr {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not contain %q", msg, want)
				}
			}
			for _, secret := range []string{testPassword, testJWTSecret, testCSRFSecret} {
				if strings.Contains(msg, secret) {
					t.Errorf("error message leaks a secret: %q", msg)
				}
			}
		})
	}
}

func TestLoadDBIgnoresAuthVariables(t *testing.T) {
	env := validEnv()
	delete(env, "JWT_SECRET")
	delete(env, "CSRF_SECRET")
	delete(env, "ALLOWED_ORIGINS")

	db, err := LoadDB(lookupFrom(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if db.Name != "appdb" {
		t.Fatalf("db = %+v", db.LogValue())
	}
}

func TestLogValuesOmitSecrets(t *testing.T) {
	cfg, err := Load(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logged := cfg.DB.LogValue().String() + cfg.Auth.LogValue().String()
	for _, secret := range []string{testPassword, testJWTSecret, testCSRFSecret} {
		if strings.Contains(logged, secret) {
			t.Fatalf("LogValue includes a secret: %s", logged)
		}
	}
}

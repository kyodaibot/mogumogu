package config

import (
	"strings"
	"testing"
)

const testPassword = "test-placeholder-pw"

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"DB_HOST":     "localhost",
		"DB_PORT":     "5432",
		"DB_USER":     "app",
		"DB_PASSWORD": testPassword,
		"DB_NAME":     "appdb",
	}
}

func TestLoadValidWithDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Config{
		AppPort: 8080,
		DB: DBConfig{
			Host: "localhost", Port: 5432, User: "app",
			Password: testPassword, Name: "appdb", SSLMode: "disable",
		},
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	env := validEnv()
	env["APP_PORT"] = "9090"
	env["DB_SSLMODE"] = "require"

	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppPort != 9090 || cfg.DB.SSLMode != "require" {
		t.Fatalf("got AppPort=%d SSLMode=%q", cfg.AppPort, cfg.DB.SSLMode)
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
				delete(env, "DB_NAME")
			},
			wantErr: []string{"DB_USER", "DB_NAME"},
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
			if strings.Contains(msg, testPassword) {
				t.Errorf("error message leaks the password: %q", msg)
			}
		})
	}
}

func TestDBConfigLogValueOmitsPassword(t *testing.T) {
	cfg, err := Load(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(cfg.DB.LogValue().String(), testPassword) {
		t.Fatal("LogValue includes the password")
	}
}

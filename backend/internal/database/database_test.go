package database

import (
	"context"
	"net/url"
	"testing"
	"time"

	"mogumogu/backend/internal/config"
)

func TestDSNEscapesCredentials(t *testing.T) {
	cfg := config.DBConfig{
		Host: "db.local", Port: 5433, User: "app user",
		Password: "p@ss/w:rd?", Name: "appdb", SSLMode: "require",
	}

	u, err := url.Parse(dsn(cfg))
	if err != nil {
		t.Fatalf("dsn does not parse: %v", err)
	}
	pw, _ := u.User.Password()
	if u.User.Username() != cfg.User || pw != cfg.Password {
		t.Fatalf("credentials did not round-trip: %q / %q", u.User.Username(), pw)
	}
	if u.Host != "db.local:5433" || u.Path != "/appdb" || u.Query().Get("sslmode") != "require" {
		t.Fatalf("unexpected dsn parts: host=%q path=%q query=%q", u.Host, u.Path, u.RawQuery)
	}
}

func TestOpenDoesNotConnect(t *testing.T) {
	// Nothing listens on port 1, so any connection attempt would fail.
	cfg := config.DBConfig{
		Host: "127.0.0.1", Port: 1, User: "app",
		Password: "test-placeholder-pw", Name: "appdb", SSLMode: "disable",
	}

	_, sqlDB, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open should not need a reachable database: %v", err)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err == nil {
		t.Fatal("expected ping to fail without a database")
	}
}

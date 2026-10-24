// Package testdb gives integration tests a private, fully migrated database.
//
// Tests using it are skipped unless TEST_DATABASE_URL points at a PostgreSQL
// server where the user may create databases (the compose superuser can).
// When CI is set they fail instead of skipping.
// `make test-integration` sets it from .env; nothing here reads .env itself.
package testdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx" database/sql driver for the admin connection.
	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm"

	"mogumogu/backend/internal/database"
)

const envVar = "TEST_DATABASE_URL"

// DB is a temporary database that is dropped when the test ends.
type DB struct {
	Gorm *gorm.DB
	SQL  *sql.DB
	// URL connects to the temporary database, for opening extra connections.
	URL string
}

// New creates a database named mogumogu_test_<random>, applies all migrations
// and registers cleanup that drops it.
func New(t *testing.T) *DB {
	t.Helper()
	adminURL := os.Getenv(envVar)
	if adminURL == "" {
		// CI (GitHub Actions sets CI=true) must run these tests; a silent
		// skip there would hide a broken database setup.
		if os.Getenv("CI") != "" {
			t.Fatal(envVar + " is not set; integration tests must not be skipped in CI")
		}
		t.Skip("integration test skipped: " + envVar + " is not set (run `make test-integration`)")
	}

	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatalf("testdb: open admin connection: %v", err)
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		t.Fatalf("testdb: cannot reach PostgreSQL via %s (is `make db` running?): %v", envVar, err)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "mogumogu_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatalf("testdb: create database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
			t.Errorf("testdb: drop %s: %v", name, err)
		}
		admin.Close()
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("testdb: parse %s: %v", envVar, err)
	}
	u.Path = "/" + name
	dbURL := u.String()

	m, err := database.NewMigratorURL(dbURL)
	if err != nil {
		t.Fatalf("testdb: migrator: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("testdb: migrate up: %v", err)
	}
	m.Close()

	gdb, sqlDB, err := database.OpenURL(dbURL)
	if err != nil {
		t.Fatalf("testdb: open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return &DB{Gorm: gdb, SQL: sqlDB, URL: dbURL}
}

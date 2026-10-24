package database

import (
	"errors"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Registers the pgx5:// database driver.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"mogumogu/backend/internal/config"
	"mogumogu/backend/migrations"
)

// NewMigrator returns a migrator for the configured database that applies the
// embedded SQL files in backend/migrations.
func NewMigrator(cfg config.DBConfig) (*migrate.Migrate, error) {
	m, err := NewMigratorURL(dsn(cfg))
	if err != nil {
		return nil, Redact(err, cfg.Password)
	}
	return m, nil
}

// NewMigratorURL is NewMigrator for a postgres:// URL (used by integration tests).
func NewMigratorURL(postgresURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	// golang-migrate selects its pgx v5 driver by the pgx5:// scheme.
	return migrate.NewWithSourceInstance("iofs", src, "pgx5://"+strings.TrimPrefix(postgresURL, "postgres://"))
}

// Redact removes the password from driver errors, which may echo the URL.
func Redact(err error, password string) error {
	if password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), password, "****"))
}

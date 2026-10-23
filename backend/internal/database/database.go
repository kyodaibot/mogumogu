// Package database opens the GORM connection pool.
package database

import (
	"database/sql"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"mogumogu/backend/internal/config"
)

// Open creates the connection pool without connecting. A database that is
// down at startup is reported by readiness instead of failing the process;
// database/sql reconnects on its own once the database is back.
func Open(cfg config.DBConfig) (*gorm.DB, *sql.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn(cfg)), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		// Driver errors can echo the connection string, so keep them out.
		return nil, nil, errors.New("open database: invalid connection settings")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	return db, sqlDB, nil
}

// dsn builds a postgres URL; url.URL escapes credentials safely.
// The result contains the password and must never be logged.
func dsn(cfg config.DBConfig) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     "/" + cfg.Name,
		RawQuery: url.Values{"sslmode": {cfg.SSLMode}}.Encode(),
	}
	return u.String()
}

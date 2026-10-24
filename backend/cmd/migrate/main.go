// Command migrate applies the numbered SQL migrations in backend/migrations.
//
//	go run ./cmd/migrate up             apply all pending migrations
//	go run ./cmd/migrate down           roll back one migration
//	go run ./cmd/migrate version        print the current version
//	go run ./cmd/migrate create <name>  add an empty up/down pair
//
// Database settings come from the same DB_* environment variables as the server.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/golang-migrate/migrate/v4"

	"mogumogu/backend/internal/config"
	"mogumogu/backend/internal/database"
)

const migrationsDir = "migrations"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: migrate up | down | version | create <name>")
	}
	if args[0] == "create" {
		if len(args) != 2 {
			return errors.New("usage: migrate create <name>")
		}
		return create(args[1])
	}

	cfg, err := config.DBFromEnv()
	if err != nil {
		return err
	}
	m, err := database.NewMigrator(cfg)
	if err != nil {
		return err
	}
	defer m.Close()

	switch args[0] {
	case "up":
		err = m.Up()
	case "down":
		// On an empty database golang-migrate reports "file does not exist".
		if _, _, verr := m.Version(); errors.Is(verr, migrate.ErrNilVersion) {
			fmt.Println("nothing to roll back")
			return nil
		}
		err = m.Steps(-1)
	case "version":
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("no change")
		err = nil
	}
	if err != nil {
		return database.Redact(err, cfg.Password)
	}

	version, dirty, err := m.Version()
	switch {
	case errors.Is(err, migrate.ErrNilVersion):
		fmt.Println("version: none (empty database)")
	case err != nil:
		return database.Redact(err, cfg.Password)
	default:
		fmt.Printf("version: %d (dirty: %t)\n", version, dirty)
	}
	return nil
}

var namePattern = regexp.MustCompile(`^[a-z0-9_]+$`)
var filePattern = regexp.MustCompile(`^(\d+)_.*\.(up|down)\.sql$`)

// create writes the next numbered, empty up/down pair.
func create(name string) error {
	if !namePattern.MatchString(name) {
		return errors.New("name must contain only a-z, 0-9 and _")
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("run this from the backend directory: %w", err)
	}
	next := 1
	for _, e := range entries {
		if m := filePattern.FindStringSubmatch(e.Name()); m != nil {
			if n, _ := strconv.Atoi(m[1]); n >= next {
				next = n + 1
			}
		}
	}
	for _, dir := range []string{"up", "down"} {
		path := filepath.Join(migrationsDir, fmt.Sprintf("%06d_%s.%s.sql", next, name, dir))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		f.Close()
		fmt.Println("created", path)
	}
	return nil
}

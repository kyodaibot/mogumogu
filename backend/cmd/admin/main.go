// Command admin changes what only an administrator may change. It talks to
// the database directly and is not reachable over HTTP (plans/archive/account-tiers.md).
//
//	go run ./cmd/admin users                          list users and their tiers
//	go run ./cmd/admin tier <username> <normal|pro>   change a user's tier
//
// Database settings come from the same DB_* environment variables as the server.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"mogumogu/backend/internal/config"
	"mogumogu/backend/internal/database"
	"mogumogu/backend/internal/store"
	"mogumogu/backend/internal/tz"
)

const usage = "usage: admin users | admin tier <username> <normal|pro>"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	cmd, err := parse(args)
	if err != nil {
		return err
	}
	cfg, err := config.DBFromEnv()
	if err != nil {
		return err
	}
	gormDB, sqlDB, err := database.Open(cfg)
	if err != nil {
		return database.Redact(err, cfg.Password)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := cmd(ctx, store.New(gormDB), out); err != nil {
		return database.Redact(err, cfg.Password)
	}
	return nil
}

type command func(ctx context.Context, st *store.Store, out io.Writer) error

// parse checks the arguments before anything connects to the database.
func parse(args []string) (command, error) {
	switch {
	case len(args) == 1 && args[0] == "users":
		return listUsers, nil
	case len(args) == 3 && args[0] == "tier":
		username := strings.ToLower(args[1])
		tier, ok := store.ParseTier(args[2])
		if !ok {
			return nil, fmt.Errorf("unknown tier %q; use normal or pro", args[2])
		}
		return func(ctx context.Context, st *store.Store, out io.Writer) error {
			return setTier(ctx, st, out, username, tier)
		}, nil
	}
	return nil, errors.New(usage)
}

func listUsers(ctx context.Context, st *store.Store, out io.Writer) error {
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tTIER\tCREATED")
	for _, u := range users {
		fmt.Fprintf(w, "%s\t%s\t%s\n", u.Username, u.Tier, tz.FormatInstant(u.CreatedAt))
	}
	return w.Flush()
}

func setTier(ctx context.Context, st *store.Store, out io.Writer, username string, tier store.Tier) error {
	err := st.SetUserTier(ctx, username, tier)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no user named %q", username)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s is now %s\n", username, tier)
	return nil
}

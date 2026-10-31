package database_test

import (
	"testing"

	"mogumogu/backend/internal/database"
	"mogumogu/backend/internal/testdb"
)

// Users who signed up before account tiers existed become normal.
func TestIntegrationTierMigrationKeepsExistingUsers(t *testing.T) {
	db := testdb.New(t)
	m, err := database.NewMigratorURL(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := m.Migrate(6); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO users (username, password_hash) VALUES ('early', 'x')`); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(7); err != nil {
		t.Fatal(err)
	}
	var tier string
	if err := db.SQL.QueryRow(`SELECT tier FROM users WHERE username = 'early'`).Scan(&tier); err != nil {
		t.Fatal(err)
	}
	if tier != "normal" {
		t.Fatalf("existing user's tier: %q", tier)
	}
}

//go:build integration

package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// integrationDB creates a new database, never a schema in an existing database.
// Some migrations inspect cluster-wide catalog names without restricting schema.
// The configured role must have CREATEDB. Only the randomly named database that
// this invocation successfully creates is ever dropped.
func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to a PostgreSQL role with CREATEDB")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		dsn, err = pq.ParseURL(dsn)
		if err != nil {
			t.Fatal(err)
		}
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	name := fmt.Sprintf("ekster_test_%x", suffix)
	if _, err := admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name) + " TEMPLATE template0"); err != nil {
		admin.Close()
		t.Fatalf("create isolated database: %v", err)
	}
	var db *sql.DB
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		if _, err := admin.Exec("DROP DATABASE " + pq.QuoteIdentifier(name)); err != nil {
			t.Errorf("drop isolated database %s: %v", name, err)
		}
		admin.Close()
	})
	isolatedDSN := dsn + " dbname=" + name
	migrationDB, err := sql.Open("postgres", isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(migrationDB); err != nil {
		migrationDB.Close()
		t.Fatalf("run migrations: %v", err)
	}
	// runMigrations closes its supplied database via migrate.Close.
	db, err = sql.Open("postgres", isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}

// testChannel describes a channel fixture by its public identity; ids are
// assigned in order so tests that assert on item ids stay readable.
type testChannel struct {
	UID  string
	Name string
	User int
}

// seedChannels replaces the migration-created default channels. Explicit ids
// are inserted with OVERRIDING SYSTEM VALUE because the real columns are
// GENERATED ALWAYS AS IDENTITY. Items, feeds and mutes go with the channels.
func seedChannels(t *testing.T, db *sql.DB, channels ...testChannel) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM channels`); err != nil {
		t.Fatal(err)
	}
	for i, c := range channels {
		name := c.Name
		if name == "" {
			name = c.UID
		}
		user := c.User
		if user == 0 {
			user = 1
		}
		_, err := db.Exec(`INSERT INTO channels (id, uid, name, user_id)
			OVERRIDING SYSTEM VALUE VALUES ($1, $2, $3, $4)`, i+1, c.UID, name, user)
		if err != nil {
			t.Fatalf("seed channel %s: %v", c.UID, err)
		}
	}
	syncIdentity(t, db, "channels")
}

// seedUsers creates users with the ids tests refer to. The users table has no
// rows from the migrations, and mutes.user_id references it.
func seedUsers(t *testing.T, db *sql.DB, urls ...string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	for i, u := range urls {
		_, err := db.Exec(`INSERT INTO users (id, url, me, token_endpoint)
			OVERRIDING SYSTEM VALUE VALUES ($1, $2, $2, $3)`, i+1, u, u+"token")
		if err != nil {
			t.Fatalf("seed user %s: %v", u, err)
		}
	}
	syncIdentity(t, db, "users")
}

// syncIdentity advances a table's identity sequence past explicitly inserted
// ids. Tests insert fixed ids so they can assert on them; without this the next
// generated id would collide with a fixture row.
func syncIdentity(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var seq sql.NullString
	if err := db.QueryRow(`SELECT pg_get_serial_sequence($1, 'id')`, table).Scan(&seq); err != nil {
		t.Fatalf("identity sequence for %s: %v", table, err)
	}
	if !seq.Valid {
		return
	}
	var max sql.NullInt64
	if err := db.QueryRow(`SELECT max(id) FROM ` + table).Scan(&max); err != nil {
		t.Fatal(err)
	}
	if max.Valid {
		if _, err := db.Exec(`SELECT setval($1, $2)`, seq.String, max.Int64); err != nil {
			t.Fatal(err)
		}
	}
}

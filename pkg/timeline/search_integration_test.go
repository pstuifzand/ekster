//go:build integration
// +build integration

package timeline

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

// TestPostgresItemsByUIDSkipsMissing exercises the search path: the bleve index
// can hold uids that no longer exist in "items" (deleted items, an index that
// outlived a channel). One stale hit used to fail the whole search with
// ErrItemNotFound, and a search whose hits are all stale answered an error
// instead of an empty list.
func TestPostgresItemsByUIDSkipsMissing(t *testing.T) {
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to run the PostgreSQL item search integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
CREATE TEMP TABLE items (
  id serial PRIMARY KEY,
  channel_id integer NOT NULL,
  uid text NOT NULL UNIQUE,
  data jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  is_read integer NOT NULL,
  published_at timestamptz NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}

	published := time.Date(2026, 11, 11, 22, 22, 16, 0, time.UTC)
	_, err = db.Exec(`INSERT INTO items (channel_id, uid, data, created_at, is_read, published_at)
VALUES (1, 'real-uid', '{"type":"entry","name":"Sit Like a Buddha"}'::jsonb, $1, 0, $1)`, published)
	if err != nil {
		t.Fatal(err)
	}

	stream := &postgresStream{database: db, channelID: 1}

	items, err := stream.ItemsByUID([]string{"stale-uid", "real-uid"})
	if err != nil {
		t.Fatalf("ItemsByUID with a stale uid: %v", err)
	}
	if len(items) != 1 || items[0].ID != "real-uid" {
		t.Fatalf("ItemsByUID returned %#v, want only real-uid", items)
	}

	empty, err := stream.ItemsByUID([]string{"stale-uid"})
	if err != nil {
		t.Fatalf("ItemsByUID with only stale uids: %v", err)
	}
	if empty == nil {
		t.Fatal("ItemsByUID returned nil, want an empty slice")
	}
	if len(empty) != 0 {
		t.Fatalf("ItemsByUID returned %d items, want 0", len(empty))
	}
}

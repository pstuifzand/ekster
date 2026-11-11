//go:build integration
// +build integration

package timeline

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestPostgresItemsPagesEqualTimestamps exercises the actual SQL paging path.
// A page boundary falls among entries with identical published_at values, which
// used to make timestamp-only cursors skip the remaining rows.
func TestPostgresItemsPagesEqualTimestamps(t *testing.T) {
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to run the PostgreSQL pagination integration test")
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

	published := time.Date(2026, 11, 9, 19, 17, 19, 0, time.UTC)
	const count = 45
	for i := 0; i < count; i++ {
		_, err := db.Exec(`INSERT INTO items (channel_id, uid, data, created_at, is_read, published_at)
VALUES (1, $1, '{}'::jsonb, $2, 0, $3)`, fmt.Sprintf("item-%02d", i), published, published)
		if err != nil {
			t.Fatal(err)
		}
	}

	stream := &postgresStream{database: db, channelID: 1}
	seen := make(map[string]bool, count)
	cursor := ""
	for {
		page, err := stream.Items("", cursor)
		if err != nil {
			t.Fatalf("Items(after=%q): %v", cursor, err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("item %q appeared on more than one page", item.ID)
			}
			seen[item.ID] = true
		}
		if page.Paging.After == "" {
			break
		}
		if page.Paging.After == cursor {
			t.Fatalf("paging cursor did not advance: %q", cursor)
		}
		cursor = page.Paging.After
	}

	if len(seen) != count {
		t.Fatalf("paged through %d items, want all %d; missing entries with equal timestamps", len(seen), count)
	}
}

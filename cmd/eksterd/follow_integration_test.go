//go:build integration
// +build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// TestFollowURLDuplicate exercises the real SQL behind FollowURL. "feeds"."url"
// is unique, so following a URL that is already followed must not surface a
// constraint violation as a 500: it is a no-op inside the same channel and
// ErrFeedAlreadyFollowed in another channel.
func TestFollowURLDuplicate(t *testing.T) {
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to run the PostgreSQL follow integration test")
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

	// TEMP tables shadow the real ones for the duration of the connection.
	for _, stmt := range []string{
		`CREATE TEMP TABLE channels (id serial PRIMARY KEY, uid text NOT NULL UNIQUE, name text NOT NULL)`,
		`CREATE TEMP TABLE feeds (
			id serial PRIMARY KEY,
			channel_id integer NOT NULL,
			url character varying(512) NOT NULL UNIQUE,
			tier integer,
			unmodified integer,
			next_fetch_at timestamptz
		)`,
		`INSERT INTO channels (uid, name) VALUES ('chan-a', 'A'), ('chan-b', 'B')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	const feedURL = "https://ramblinggit.com/"
	if _, err := db.Exec(`INSERT INTO feeds (channel_id, url, tier, unmodified, next_fetch_at)
VALUES (1, $1, 1, 0, now())`, feedURL); err != nil {
		t.Fatalf("seed: %v", err)
	}

	backend := &memoryBackend{database: db}
	ctx := context.Background()

	// Same channel: idempotent, no error, no second row.
	if _, err := backend.FollowURL(ctx, "chan-a", feedURL); err != nil {
		t.Fatalf("re-follow in the same channel: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM feeds WHERE url = $1`, feedURL).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("feeds rows for %s = %d, want 1", feedURL, count)
	}

	// Another channel: a clean, typed conflict instead of a driver error.
	_, err = backend.FollowURL(ctx, "chan-b", feedURL)
	if !errors.Is(err, microsub.ErrFeedAlreadyFollowed) {
		t.Fatalf("follow in another channel: err = %v, want ErrFeedAlreadyFollowed", err)
	}

	// The feed must still belong to the channel that owns it.
	var channelID int
	if err := db.QueryRow(`SELECT channel_id FROM feeds WHERE url = $1`, feedURL).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if channelID != 1 {
		t.Fatalf("feed moved to channel_id %d, want 1", channelID)
	}

	// A channel without follows must answer an empty list, not nil: its JSON is
	// the follow list an MCP tool validates against an array schema.
	feeds, err := backend.FollowGetList(ctx, "chan-b")
	if err != nil {
		t.Fatalf("FollowGetList: %v", err)
	}
	if feeds == nil {
		t.Fatal("FollowGetList returned nil for an empty channel, want an empty slice")
	}
	if len(feeds) != 0 {
		t.Fatalf("FollowGetList on an empty channel returned %d feeds", len(feeds))
	}
}

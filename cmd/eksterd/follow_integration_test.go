//go:build integration
// +build integration

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// TestFollowURLDuplicate exercises the real SQL behind FollowURL. "feeds"."url"
// is unique, so following a URL that is already followed must not surface a
// constraint violation as a 500: it is a no-op inside the same channel and
// ErrFeedAlreadyFollowed in another channel.
func TestFollowURLDuplicate(t *testing.T) {
	db := integrationDB(t)

	seedChannels(t, db, testChannel{UID: "chan-a", Name: "A"}, testChannel{UID: "chan-b", Name: "B"})

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
	_, err := backend.FollowURL(ctx, "chan-b", feedURL)
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

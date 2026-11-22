//go:build integration
// +build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// A host that answers 502 for a moment costs one short wait, and a feed that
// answers 404 costs the ordinary two minutes. The schedule is what the next
// attempt actually does, so assert on the row the scheduler reads, not on the
// helper that computed it.
func TestFailureScheduleReachesTheFeedRow(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db, testChannel{UID: "chan-a", Name: "A"})

	newFeed := func(url string, failures int) int {
		var id int
		if err := db.QueryRow(`INSERT INTO feeds (channel_id, url, tier, consecutive_failures)
			VALUES (1, $1, 6, $2) RETURNING id`, url, failures).Scan(&id); err != nil {
			t.Fatalf("insert feed: %v", err)
		}
		return id
	}

	// nextFetchAt reads the row back the way the scheduler does.
	nextFetchAt := func(id int) (time.Time, int) {
		var next time.Time
		var failures int
		if err := db.QueryRow(`SELECT next_fetch_at, consecutive_failures FROM feeds WHERE id = $1`, id).
			Scan(&next, &failures); err != nil {
			t.Fatalf("read feed row: %v", err)
		}
		return next, failures
	}

	serve := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
	}

	backend := &memoryBackend{database: db}
	ctx := context.Background()

	t.Run("502 comes back in half a minute", func(t *testing.T) {
		server := serve(http.StatusBadGateway)
		defer server.Close()

		id := newFeed(server.URL, 0)
		if err := backend.refreshFeed(ctx, feed{ID: id, UID: "chan-a", URL: server.URL}); err == nil {
			t.Fatal("a 502 was not reported as a failure")
		}

		next, failures := nextFetchAt(id)
		if failures != 1 {
			t.Errorf("consecutive_failures = %d, want 1", failures)
		}
		if wait := time.Until(next); wait < 20*time.Second || wait > 40*time.Second {
			t.Errorf("next attempt in %s, want about %s", wait, transientFailureFirstDelay)
		}
	})

	t.Run("404 still waits the ordinary two minutes", func(t *testing.T) {
		server := serve(http.StatusNotFound)
		defer server.Close()

		id := newFeed(server.URL, 0)
		if err := backend.refreshFeed(ctx, feed{ID: id, UID: "chan-a", URL: server.URL}); err == nil {
			t.Fatal("a 404 was not reported as a failure")
		}

		next, failures := nextFetchAt(id)
		if failures != 1 {
			t.Errorf("consecutive_failures = %d, want 1", failures)
		}
		if wait := time.Until(next); wait < 100*time.Second || wait > 140*time.Second {
			t.Errorf("next attempt in %s, want about %s", wait, minFailureDelay)
		}
	})

	t.Run("a second 502 climbs the ladder", func(t *testing.T) {
		server := serve(http.StatusBadGateway)
		defer server.Close()

		id := newFeed(server.URL, 1)
		// The feed struct carries the consecutive count the row does, which is
		// what the scheduler reads before the attempt.
		if err := backend.refreshFeed(ctx, feed{ID: id, UID: "chan-a", URL: server.URL, Failures: 1}); err == nil {
			t.Fatal("a 502 was not reported as a failure")
		}

		next, failures := nextFetchAt(id)
		if failures != 2 {
			t.Errorf("consecutive_failures = %d, want 2", failures)
		}
		if wait := time.Until(next); wait < 220*time.Second || wait > 260*time.Second {
			t.Errorf("second attempt waits %s, want about 4m", wait)
		}
	})
}

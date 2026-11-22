//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/timeline"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestTimelineRemoveTombstoneIntegration(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db, testChannel{UID: "one"}, testChannel{UID: "two"})
	one := timeline.Create("one", "postgres-stream", nil, db)
	two := timeline.Create("two", "postgres-stream", nil, db)
	item := microsub.Item{ID: "post-1", Published: "2026-01-01T00:00:00Z"}
	if inserted, err := one.AddItem(item); err != nil || !inserted {
		t.Fatalf("initial insert: inserted=%v err=%v", inserted, err)
	}
	backend := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	ctx := userid.NewContext(context.Background(), 1)
	if err := backend.TimelineRemove(ctx, "one", []string{"post-1", "unknown"}); err != nil {
		t.Fatal(err)
	}
	if inserted, err := one.AddItem(item); err != nil || inserted {
		t.Fatalf("redelivery: inserted=%v err=%v", inserted, err)
	}
	if inserted, err := two.AddItem(item); err != nil || !inserted {
		t.Fatalf("other channel: inserted=%v err=%v", inserted, err)
	}
	var tombstones int
	if err := db.QueryRow(`SELECT count(*) FROM item_tombstones WHERE channel_id = 1`).Scan(&tombstones); err != nil || tombstones != 1 {
		t.Fatalf("tombstones=%d err=%v", tombstones, err)
	}
	if _, err := db.Exec(`DELETE FROM channels WHERE id IN (1, 2)`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM item_tombstones`).Scan(&tombstones); err != nil || tombstones != 0 {
		t.Fatalf("tombstones after channel deletion=%d err=%v", tombstones, err)
	}
}

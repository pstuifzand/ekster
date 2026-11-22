//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestMarkReadThroughIsolationIntegration(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db,
		testChannel{UID: "shared", Name: "Shared"},
		testChannel{UID: "other", Name: "Other"},
		testChannel{UID: "shared-two", Name: "Shared two", User: 2})
	if _, err := db.Exec(`INSERT INTO items (id, channel_id, uid, published_at, is_read) OVERRIDING SYSTEM VALUE VALUES
		(1,1,'old','2024-01-01',0),(2,1,'tie-old','2024-01-02',0),(3,1,'boundary','2024-01-02',0),
		(4,1,'tie-new','2024-01-02',0),(5,1,'new','2024-01-03',0),(6,2,'other-boundary','2024-01-01',0),
		(7,3,'shared-boundary','2024-01-01',0),(8,3,'secret','2024-01-01',0)`); err != nil {
		t.Fatal(err)
	}
	b := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	h, _ := server.NewMicrosubHandler(b)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := &client.Client{MicrosubEndpoint: u}
	if err := c.MarkReadThrough(context.Background(), "shared", "secret"); err == nil {
		t.Fatal("cross-user boundary accepted")
	}
	if err := c.MarkReadThrough(context.Background(), "other", "old"); err == nil {
		t.Fatal("cross-channel boundary accepted")
	}
	if err := c.MarkReadThrough(context.Background(), "missing", "boundary"); err == nil {
		t.Fatal("missing channel accepted")
	}
	if err := c.MarkReadThrough(context.Background(), "shared", "boundary"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id,is_read FROM items ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []int{1, 1, 1, 0, 0, 0, 0, 0}
	i := 0
	for rows.Next() {
		var id, state int
		if err := rows.Scan(&id, &state); err != nil {
			t.Fatal(err)
		}
		if id != i+1 || state != want[i] {
			t.Fatalf("id %d state %d", id, state)
		}
		i++
	}
	if err := rows.Err(); err != nil || i != len(want) {
		t.Fatalf("rows=%d err=%v", i, err)
	}
	msg := <-b.broker.Notifier
	if msg.UserID == nil || *msg.UserID != 1 {
		t.Fatalf("notification %#v", msg)
	}
}

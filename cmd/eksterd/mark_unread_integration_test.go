//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestMarkUnreadChannelIsolationIntegration(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db, testChannel{UID: "one", Name: "One"}, testChannel{UID: "two", Name: "Two"})
	if _, err := db.Exec(`INSERT INTO items (id, channel_id, uid, data, created_at, is_read, published_at) OVERRIDING SYSTEM VALUE VALUES
		(1,1,'a','{"type":"entry"}',now(),1,now()),(2,1,'b','{"type":"entry"}',now(),1,now()),
		(3,2,'c','{"type":"entry"}',now(),1,now())`); err != nil {
		t.Fatal(err)
	}
	backend := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	h, _ := server.NewMicrosubHandler(backend)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := &client.Client{MicrosubEndpoint: u}
	ctx := context.Background()
	if err := c.MarkUnread(ctx, "one", []string{"a", "b", "c", "absent"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		channel string
		states  map[string]bool
	}{{"one", map[string]bool{"a": false, "b": false}}, {"two", map[string]bool{"c": true}}} {
		page, err := c.TimelineGet(ctx, "", "", tc.channel)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != len(tc.states) {
			t.Fatalf("%s items: %v", tc.channel, page.Items)
		}
		for _, item := range page.Items {
			want, ok := tc.states[item.ID]
			if !ok || item.Read != want {
				t.Fatalf("%s %s read=%v", tc.channel, item.ID, item.Read)
			}
		}
	}
	msg := <-backend.broker.Notifier
	ch, ok := msg.Object.(microsub.Channel)
	if !ok || ch.UID != "one" || ch.Unread.UnreadCount != 2 {
		t.Fatalf("count notification: %#v", msg)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM items WHERE channel_id=1 AND is_read=0`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("unread count=%d err=%v", count, err)
	}
	if err := c.MarkUnread(ctx, "one", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE items SET is_read=0 WHERE uid='c'`); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkRead(ctx, "one", []string{"a", "c"}); err != nil {
		t.Fatal(err)
	}
	var a, cstate int
	if err := db.QueryRow(`SELECT is_read FROM items WHERE uid='a'`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT is_read FROM items WHERE uid='c'`).Scan(&cstate); err != nil {
		t.Fatal(err)
	}
	if a != 1 || cstate != 0 {
		t.Fatalf("read states a=%d c=%d", a, cstate)
	}
	if err := c.MarkUnread(ctx, "missing", []string{"c"}); err == nil {
		t.Fatal("unknown channel succeeded")
	}
}

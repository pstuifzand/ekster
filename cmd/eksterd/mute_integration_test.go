//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestMuteStorageFiltersTimelineSearchAndUnreadIntegration(t *testing.T) {
	db := integrationDB(t)
	seedUsers(t, db, "https://one.example/", "https://two.example/")
	seedChannels(t, db,
		testChannel{UID: "home", Name: "Home"},
		testChannel{UID: "other", Name: "Other"},
		testChannel{UID: "second", Name: "Second", User: 2})
	for _, q := range []string{
		`INSERT INTO items (id, channel_id, uid, data, created_at, is_read, published_at) OVERRIDING SYSTEM VALUE VALUES
		 (1,1,'account','{"type":"entry","author":{"url":"https://account.example"}}',now(),0,'2026-01-03T00:00:00Z'),
		 (2,1,'channel','{"type":"entry","author":{"url":"https://channel.example"}}',now(),0,'2026-01-02T00:00:00Z'),
		 (3,1,'visible','{"type":"entry","author":{"url":"https://visible.example"}}',now(),0,'2026-01-01T00:00:00Z'),
		 (4,2,'other-channel','{"type":"entry","author":{"url":"https://channel.example"}}',now(),0,'2026-01-01T00:00:00Z'),
		 (5,3,'other-user','{"type":"entry","author":{"url":"https://account.example"}}',now(),0,'2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	b := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	user1 := userid.NewContext(context.Background(), 1)
	if err := b.MuteURL(user1, "", "https://account.example"); err != nil {
		t.Fatal(err)
	}
	if err := b.MuteURL(user1, "home", "https://channel.example"); err != nil {
		t.Fatal(err)
	}
	if err := b.MuteURL(user1, "home", "https://channel.example"); err != nil {
		t.Fatal(err)
	}
	if got, err := b.MuteGetList(user1, "home"); err != nil || len(got) != 2 {
		t.Fatalf("home mutes=%v err=%v", got, err)
	}
	if got, err := b.MuteGetList(user1, "other"); err != nil || len(got) != 1 || got[0] != "https://account.example" {
		t.Fatalf("other mutes=%v err=%v", got, err)
	}
	page, err := b.TimelineGet(user1, "", "", "home")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "visible" {
		t.Fatalf("timeline=%#v err=%v", page.Items, err)
	}
	if _, err := b.TimelineGet(userid.NewContext(context.Background(), 2), "", "", "home"); err == nil {
		t.Fatal("foreign timeline access succeeded")
	}
	search, err := b.ItemSearchWithOptions(user1, microsub.ItemSearchOptions{Channel: "global", Author: []string{"https://visible.example", "https://channel.example", "https://account.example"}, Limit: 10})
	if err != nil || len(search.Items) != 2 {
		t.Fatalf("search=%#v err=%v", search.Items, err)
	}
	if err := b.updateChannelUnreadCount(context.Background(), "home"); err != nil {
		t.Fatal(err)
	}
	msg := <-b.broker.Notifier
	ch, ok := msg.Object.(microsub.Channel)
	if !ok || msg.UserID == nil || *msg.UserID != 1 || ch.Unread.UnreadCount != 1 {
		t.Fatalf("unread event=%#v", msg)
	}
	if err := b.UnmuteURL(user1, "home", "https://channel.example"); err != nil {
		t.Fatal(err)
	}
	page, err = b.TimelineGet(user1, "", "", "home")
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("unmute timeline=%#v err=%v", page.Items, err)
	}
	if err := b.UnmuteURL(user1, "global", "https://account.example"); err != nil {
		t.Fatal(err)
	}
	page, err = b.TimelineGet(user1, "", "", "home")
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("global unmute timeline=%#v err=%v", page.Items, err)
	}
}

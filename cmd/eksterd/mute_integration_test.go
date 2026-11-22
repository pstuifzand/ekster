//go:build integration

package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestMuteStorageFiltersTimelineSearchAndUnreadIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, q := range []string{
		`CREATE TEMP TABLE users (id int primary key)`,
		`CREATE TEMP TABLE channels (id int primary key, uid text unique, name text, user_id int not null)`,
		`CREATE TEMP TABLE mutes (id int primary key generated always as identity, user_id int not null, channel_id int, url text not null)`,
		`CREATE UNIQUE INDEX mutes_account_url_unique ON mutes (user_id, url) WHERE channel_id IS NULL`,
		`CREATE UNIQUE INDEX mutes_channel_url_unique ON mutes (user_id, channel_id, url) WHERE channel_id IS NOT NULL`,
		`CREATE TEMP TABLE items (id int primary key, channel_id int, uid text unique, data jsonb, created_at timestamptz, is_read int, published_at timestamptz)`,
		`INSERT INTO users VALUES (1),(2)`,
		`INSERT INTO channels VALUES (1,'home','Home',1),(2,'other','Other',1),(3,'second','Second',2)`,
		`INSERT INTO items VALUES
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

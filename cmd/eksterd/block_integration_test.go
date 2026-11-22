//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func itemUIDs(t *testing.T, db *sql.DB, channelID int) []string {
	t.Helper()
	rows, err := db.Query(`SELECT uid FROM items WHERE channel_id = $1 ORDER BY uid`, channelID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatal(err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return uids
}

func blockedAuthorItem(id, published string) microsub.Item {
	return microsub.Item{
		ID:        id,
		Type:      "entry",
		Published: published,
		Author:    &microsub.Card{Type: "card", URL: "https://blocked.example", Name: "Blocked Person"},
	}
}

func TestBlockStorageAndIngestionIntegration(t *testing.T) {
	db := integrationDB(t)
	seedUsers(t, db, "https://one.example/", "https://two.example/")
	seedChannels(t, db,
		testChannel{UID: "home", Name: "Home"},
		testChannel{UID: "other", Name: "Other"},
		testChannel{UID: "second", Name: "Second", User: 2})
	for _, q := range []string{
		`INSERT INTO items (id, channel_id, uid, data, created_at, is_read, published_at) OVERRIDING SYSTEM VALUE VALUES
		 (1,1,'blocked-1','{"type":"entry","author":{"url":"https://blocked.example","name":"Blocked Person","photo":"https://blocked.example/photo.png"}}',now(),0,'2026-01-05T00:00:00Z'),
		 (2,1,'visible','{"type":"entry","author":{"url":"https://visible.example","name":"Visible Person"}}',now(),0,'2026-01-04T00:00:00Z'),
		 (3,2,'blocked-other','{"type":"entry","author":{"url":"https://blocked.example","name":"Blocked Person","photo":"https://blocked.example/photo.png"}}',now(),0,'2026-01-03T00:00:00Z'),
		 (4,3,'other-user','{"type":"entry","author":{"url":"https://blocked.example","name":"Blocked Person"}}',now(),0,'2026-01-02T00:00:00Z'),
		 (5,1,'blocked-2','{"type":"entry","author":{"url":"https://blocked.example","name":"Blocked Person"}}',now(),0,'2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	syncIdentity(t, db, "items")
	b := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	user1 := userid.NewContext(context.Background(), 1)
	user2 := userid.NewContext(context.Background(), 2)

	if err := b.BlockURL(user1, "", "not a url"); !errors.Is(err, microsub.ErrInvalidProfileURL) {
		t.Fatalf("invalid URL error=%v, want ErrInvalidProfileURL", err)
	}
	if err := b.BlockURL(user1, "foreign", "https://blocked.example"); !errors.Is(err, microsub.ErrNotFound) {
		t.Fatalf("foreign channel error=%v, want ErrNotFound", err)
	}
	if _, err := b.BlockGetList(user1, "foreign"); !errors.Is(err, microsub.ErrNotFound) {
		t.Fatalf("foreign channel list error=%v, want ErrNotFound", err)
	}

	// A channel block removes that author's items from that channel only.
	if err := b.BlockURL(user1, "home", "https://blocked.example"); err != nil {
		t.Fatal(err)
	}
	if got := itemUIDs(t, db, 1); len(got) != 1 || got[0] != "visible" {
		t.Fatalf("home items after channel block=%v", got)
	}
	if got := itemUIDs(t, db, 2); len(got) != 1 || got[0] != "blocked-other" {
		t.Fatalf("other items after channel block=%v", got)
	}
	if got := itemUIDs(t, db, 3); len(got) != 1 || got[0] != "other-user" {
		t.Fatalf("second user's items after channel block=%v", got)
	}

	// The card carries the most recent stored item's author name and photo.
	cards, err := b.BlockGetList(user1, "home")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].Type != "card" || cards[0].URL != "https://blocked.example" ||
		cards[0].Name != "Blocked Person" || cards[0].Photo != "https://blocked.example/photo.png" {
		t.Fatalf("home blocks=%#v", cards)
	}
	if cards, err := b.BlockGetList(user1, "other"); err != nil || len(cards) != 0 {
		t.Fatalf("other blocks=%#v err=%v", cards, err)
	}
	if cards, err := b.BlockGetList(user1, "global"); err != nil || len(cards) != 0 {
		t.Fatalf("global blocks=%#v err=%v", cards, err)
	}

	page, err := b.TimelineGet(user1, "", "", "home")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "visible" {
		t.Fatalf("home timeline=%#v err=%v", page.Items, err)
	}
	page, err = b.TimelineGet(user1, "", "", "other")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "blocked-other" {
		t.Fatalf("other timeline=%#v err=%v", page.Items, err)
	}

	// A later item from a blocked author is not stored in the blocked channel,
	// but the same author is still stored in another channel of that user.
	added, err := b.channelAddItemWithMatcher(user1, "home", blockedAuthorItem("blocked-new", "2026-01-06T00:00:00Z"), nil)
	if err != nil || added {
		t.Fatalf("blocked item added=%v err=%v", added, err)
	}
	if got := itemUIDs(t, db, 1); len(got) != 1 || got[0] != "visible" {
		t.Fatalf("home items after blocked add=%v", got)
	}
	added, err = b.channelAddItemWithMatcher(user1, "other", blockedAuthorItem("other-new", "2026-01-06T00:00:00Z"), nil)
	if err != nil || !added {
		t.Fatalf("item in unblocked channel added=%v err=%v", added, err)
	}

	// Unblocking the channel restores ingestion there.
	if err := b.UnblockURL(user1, "home", "https://blocked.example"); err != nil {
		t.Fatal(err)
	}
	added, err = b.channelAddItemWithMatcher(user1, "home", blockedAuthorItem("blocked-new-2", "2026-01-07T00:00:00Z"), nil)
	if err != nil || !added {
		t.Fatalf("item after channel unblock added=%v err=%v", added, err)
	}

	// An account-wide block affects every channel of that user and nobody else.
	if err := b.BlockURL(user1, "global", "https://blocked.example"); err != nil {
		t.Fatal(err)
	}
	if got := itemUIDs(t, db, 1); len(got) != 1 || got[0] != "visible" {
		t.Fatalf("home items after global block=%v", got)
	}
	if got := itemUIDs(t, db, 2); len(got) != 0 {
		t.Fatalf("other items after global block=%v", got)
	}
	if got := itemUIDs(t, db, 3); len(got) != 1 || got[0] != "other-user" {
		t.Fatalf("second user's items after global block=%v", got)
	}
	page, err = b.TimelineGet(user1, "", "", "other")
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("other timeline after global block=%#v err=%v", page.Items, err)
	}
	if cards, err := b.BlockGetList(user1, "global"); err != nil || len(cards) != 1 || cards[0].URL != "https://blocked.example" || cards[0].Name != "" {
		t.Fatalf("global blocks after global block=%#v err=%v", cards, err)
	}
	added, err = b.channelAddItemWithMatcher(user1, "home", blockedAuthorItem("blocked-new-3", "2026-01-08T00:00:00Z"), nil)
	if err != nil || added {
		t.Fatalf("blocked item after global block added=%v err=%v", added, err)
	}
	added, err = b.channelAddItemWithMatcher(user2, "second", blockedAuthorItem("user2-new", "2026-01-08T00:00:00Z"), nil)
	if err != nil || !added {
		t.Fatalf("another user's blocked author added=%v err=%v", added, err)
	}

	// A row that predates the block, or races with it, is still hidden by the
	// read predicates, not only by deletion and ingestion.
	if _, err := db.Exec(`INSERT INTO items (channel_id, uid, data, published_at)
		VALUES (1, 'raced', '{"type":"entry","author":{"url":"https://blocked.example"}}', '2026-01-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	page, err = b.TimelineGet(user1, "", "", "home")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "visible" {
		t.Fatalf("home timeline with raced block=%#v err=%v", page.Items, err)
	}
	search, err := b.ItemSearchWithOptions(user1, microsub.ItemSearchOptions{Channel: "global", Author: []string{"https://blocked.example"}, Limit: 10})
	if err != nil || len(search.Items) != 0 {
		t.Fatalf("search with raced block=%#v err=%v", search.Items, err)
	}
	channels, err := b.ChannelsGetList(user1)
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range channels {
		if channel.UID == "home" && channel.Unread.UnreadCount != 1 {
			t.Fatalf("home unread with raced block=%d, want 1", channel.Unread.UnreadCount)
		}
	}

	// A global unblock restores ingestion for the account.
	if err := b.UnblockURL(user1, "global", "https://blocked.example"); err != nil {
		t.Fatal(err)
	}
	added, err = b.channelAddItemWithMatcher(user1, "home", blockedAuthorItem("blocked-new-4", "2026-01-09T00:00:00Z"), nil)
	if err != nil || !added {
		t.Fatalf("item after global unblock added=%v err=%v", added, err)
	}
	if cards, err := b.BlockGetList(user1, ""); err != nil || len(cards) != 0 {
		t.Fatalf("blocks after unblock=%#v err=%v", cards, err)
	}
}

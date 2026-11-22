//go:build integration

package main

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/server"
)

func TestTimelineLimitIntegration(t *testing.T) {
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
		`CREATE TEMP TABLE channels (id int primary key, uid text)`,
		`CREATE TEMP TABLE feeds (id int, channel_id int, url text)`,
		`CREATE TEMP TABLE items (id int primary key, channel_id int, uid text, data jsonb, created_at timestamptz, is_read int, published_at timestamptz)`,
		`INSERT INTO channels VALUES (1,'home')`,
		`INSERT INTO items SELECT i,1,i::text,'{"type":"entry"}'::jsonb,now(),0,'2026-01-01T00:00:00Z'::timestamptz FROM generate_series(1, 105) i`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	h, _ := server.NewMicrosubHandler(&memoryBackend{database: db})
	s := httptest.NewServer(h)
	defer s.Close()
	u, _ := url.Parse(s.URL)
	c := &client.Client{MicrosubEndpoint: u}
	ctx := context.Background()
	legacy, err := c.TimelineGet(ctx, "", "", "home")
	if err != nil || len(legacy.Items) != 20 {
		t.Fatalf("default: %d %v", len(legacy.Items), err)
	}
	capped, err := c.TimelineGetWithLimit(ctx, "", "", "home", 1000)
	if err != nil || len(capped.Items) != 100 {
		t.Fatalf("cap: %d %v", len(capped.Items), err)
	}
	seen := map[string]bool{}
	after := ""
	for {
		page, err := c.TimelineGetWithLimit(ctx, "", after, "home", 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 7 {
			t.Fatal("page too large")
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate %s", item.ID)
			}
			seen[item.ID] = true
		}
		if page.Paging.After == "" {
			break
		}
		if page.Paging.After == after {
			t.Fatal("cursor did not advance")
		}
		after = page.Paging.After
	}
	if len(seen) != 105 {
		t.Fatalf("seen %d", len(seen))
	}
	// Bound a newer-items poll with the oldest cursor of a page; no older items leak in.
	top, err := c.TimelineGetWithLimit(ctx, "", "", "home", 10)
	if err != nil {
		t.Fatal(err)
	}
	middle, err := c.TimelineGetWithLimit(ctx, "", top.Paging.After, "home", 10)
	if err != nil {
		t.Fatal(err)
	}
	bounded, err := c.TimelineGetWithLimit(ctx, middle.Paging.After, top.Paging.After, "home", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Items) != 9 || bounded.Paging.After != "" {
		t.Fatalf("bounded: %d after=%q", len(bounded.Items), bounded.Paging.After)
	}
}

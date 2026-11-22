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
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestTimelineLimitIntegration(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db, testChannel{UID: "home", Name: "Home"})
	if _, err := db.Exec(`INSERT INTO items (id, channel_id, uid, data, created_at, is_read, published_at) OVERRIDING SYSTEM VALUE
		SELECT i,1,i::text,'{"type":"entry"}'::jsonb,now(),0,'2026-01-01T00:00:00Z'::timestamptz FROM generate_series(1, 105) i`); err != nil {
		t.Fatal(err)
	}
	h, _ := server.NewMicrosubHandler(&memoryBackend{database: db})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
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

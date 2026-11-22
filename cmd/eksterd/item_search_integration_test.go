//go:build integration

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/userid"
)

type indexDelegate = bleve.Index

type observingSearchIndex struct {
	indexDelegate
	calls    int
	maxIDs   int
	badScope bool
	cancel   context.CancelFunc
}

func (i *observingSearchIndex) SearchInContext(ctx context.Context, req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	i.calls++
	q, ok := req.Query.(*query.ConjunctionQuery)
	if !ok || len(q.Conjuncts) != 2 {
		return nil, fmt.Errorf("expected scoped conjunction")
	}
	ids, ok := q.Conjuncts[0].(*query.DocIDQuery)
	if !ok {
		return nil, fmt.Errorf("expected ID filter")
	}
	if len(ids.IDs) > i.maxIDs {
		i.maxIDs = len(ids.IDs)
	}
	for _, id := range ids.IDs {
		if id == "secret" {
			i.badScope = true
		}
	}
	if i.cancel != nil {
		i.cancel()
	}
	return i.indexDelegate.SearchInContext(ctx, req)
}

func TestItemSearchOptionsIntegration(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db,
		testChannel{UID: "home", Name: "Home"},
		testChannel{UID: "other", Name: "Other"},
		testChannel{UID: "private", Name: "Private", User: 2})
	for _, q := range []string{
		`INSERT INTO feeds (id, channel_id, url) OVERRIDING SYSTEM VALUE VALUES
		 (1,1,'https://example.com/feed'),(2,2,'https://other/feed'),(3,3,'https://secret/feed')`,
		`INSERT INTO items (id, channel_id, feed_id, uid, data, is_read, published_at) OVERRIDING SYSTEM VALUE
		 SELECT n,1,1,'item-'||n, jsonb_build_object('type','entry','name','unicorn','author',jsonb_build_object('url','https://alice/'),'category',jsonb_build_array('go','indieweb'),'in-reply-to',jsonb_build_array('https://target/'),'photo',jsonb_build_array('https://photo/'),'_source',jsonb_build_object('url','https://example.com/')),0,'2026-01-01T00:00:00.123456Z'::timestamptz FROM generate_series(1,130) n`,
		`UPDATE items SET data = data || '{"like-of":["x"],"repost-of":["x"],"bookmark-of":["x"],"checkin":{"name":"place"},"audio":["a"]}'::jsonb WHERE id=1`,
		`INSERT INTO items (id, channel_id, feed_id, uid, data, is_read, published_at) OVERRIDING SYSTEM VALUE VALUES
		 (131,2,2,'other','{"type":"entry","name":"unicorn","author":{"url":"https://bob/"},"category":["misc"],"video":["v"]}',0,'2026-01-02'),
		 (132,3,3,'secret','{"type":"entry","name":"unicorn","author":{"url":"https://alice/"},"category":["go"]}',0,'2026-01-03')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	syncIdentity(t, db, "feeds")
	syncIdentity(t, db, "items")
	idx, err := bleve.New(filepath.Join(t.TempDir(), "items.bleve"), bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	old := index
	index = idx
	t.Cleanup(func() { index = old })
	for n := 1; n <= 130; n++ {
		if err := idx.Index(fmt.Sprintf("item-%d", n), indexItem{Item: microsub.Item{ID: fmt.Sprintf("item-%d", n), Name: "unicorn"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"other", "secret", "stale"} {
		if err := idx.Index(id, indexItem{Item: microsub.Item{ID: id, Name: "unicorn"}}); err != nil {
			t.Fatal(err)
		}
	}
	backend := &memoryBackend{database: db}
	handler, _ := server.NewMicrosubHandler(backend)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
	defer srv.Close()
	endpoint, _ := url.Parse(srv.URL + "?tenant=test")
	c := &client.Client{MicrosubEndpoint: endpoint}
	ctx := context.Background()
	base := microsub.ItemSearchOptions{Channel: "global", Query: "unicorn", Author: []string{"https://nobody/", "https://alice/"}, Category: []string{"go", "missing"}, Kind: []string{"video", "reply"}, Source: []string{"https://example.com/feed"}, Limit: 17}
	seen := map[string]bool{}
	after := ""
	var first string
	for {
		o := base
		o.After = after
		page, err := c.ItemSearchWithOptions(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = page.Paging.Before
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
	if len(seen) != 130 {
		t.Fatalf("matched %d, want 130", len(seen))
	}
	o := base
	o.Limit = 10
	top, err := c.ItemSearchWithOptions(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	o.After = top.Paging.After
	middle, err := c.ItemSearchWithOptions(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	o.Before = middle.Paging.After
	o.After = top.Paging.After
	bounded, err := c.ItemSearchWithOptions(ctx, o)
	if err != nil || len(bounded.Items) != 9 || bounded.Paging.After != "" {
		t.Fatalf("bounded: %+v %v", bounded, err)
	}
	o = base
	o.Before = first
	page, err := c.ItemSearchWithOptions(ctx, o)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("before: %+v %v", page, err)
	}
	o = base
	o.Query = ""
	o.Limit = 1000
	page, err = c.ItemSearchWithOptions(ctx, o)
	if err != nil || len(page.Items) != 100 || page.Paging.After == "" {
		t.Fatalf("filter only/cap: %d %v", len(page.Items), err)
	}
	o = base
	o.Source = []string{"https://example.com/"}
	page, err = c.ItemSearchWithOptions(ctx, o)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("source must use feed URL: %+v %v", page, err)
	}
	o = base
	o.Channel = "private"
	page, err = c.ItemSearchWithOptions(ctx, o)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("private channel leaked: %+v %v", page, err)
	}
	o = microsub.ItemSearchOptions{Channel: "global", Kind: []string{"video"}}
	page, err = c.ItemSearchWithOptions(ctx, o)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "other" {
		t.Fatalf("video: %+v %v", page, err)
	}
	for _, kind := range []string{"like", "repost", "bookmark", "checkin", "audio"} {
		page, err := c.ItemSearchWithOptions(ctx, microsub.ItemSearchOptions{Channel: "global", Kind: []string{kind}})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != "item-1" {
			t.Fatalf("kind %s: %+v %v", kind, page, err)
		}
	}
	if _, err := backend.ItemSearchWithOptions(context.Background(), base); err == nil {
		t.Fatal("missing user accepted")
	}
	q := url.Values{"action": {"search"}, "channel": {"home"}, "category": {"go"}, "limit": {"2"}}
	resp, err := http.Get(srv.URL + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %d", resp.StatusCode)
	}
	for _, query := range []string{"action=search&channel=home", "action=search&channel=&query=x", "action=search&channel=home&query=x&limit=0", "action=search&channel=home&category=go&before=bad"} {
		r, err := http.Post(srv.URL, "application/x-www-form-urlencoded", strings.NewReader(query))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Errorf("%s: %d", query, r.StatusCode)
		}
	}
	// The spec polls newer entries with the original before, then follows
	// after within that same bound until all newly arrived items are read.
	for n := 200; n < 245; n++ {
		uid := fmt.Sprintf("new-%d", n)
		_, err := db.Exec(`INSERT INTO items (id, channel_id, feed_id, uid, data, is_read, published_at) OVERRIDING SYSTEM VALUE
			SELECT $1,1,1,$2,data,0,'2026-02-01T00:00:00.123456Z'::timestamptz FROM items WHERE id=1`, n, uid)
		if err != nil {
			t.Fatal(err)
		}
		if err := idx.Index(uid, indexItem{Item: microsub.Item{ID: uid, Name: "unicorn"}}); err != nil {
			t.Fatal(err)
		}
	}
	poll := base
	poll.Before = first
	poll.Limit = 10
	newSeen := map[string]bool{}
	for {
		page, err := c.ItemSearchWithOptions(ctx, poll)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if newSeen[item.ID] {
				t.Fatalf("duplicate new item %s", item.ID)
			}
			newSeen[item.ID] = true
		}
		if page.Paging.After == "" {
			break
		}
		poll.After = page.Paging.After
	}
	if len(newSeen) != 45 {
		t.Fatalf("newer poll returned %d of 45", len(newSeen))
	}
	for n := 200; n < 245; n++ {
		if !newSeen[fmt.Sprintf("new-%d", n)] {
			t.Fatalf("missing new-%d", n)
		}
	}

	// Many newer DB rows do not match the index. Scanning them must remain
	// bounded, retain ownership and find an older hit beyond several batches.
	_, err = db.Exec(`INSERT INTO items (id, channel_id, feed_id, uid, data, is_read, published_at) OVERRIDING SYSTEM VALUE
		SELECT n,1,1,'unindexed-'||n,(SELECT data FROM items WHERE id=1),0,'2026-03-01T00:00:00Z'::timestamptz FROM generate_series(1000,1399) n`)
	if err != nil {
		t.Fatal(err)
	}
	observed := &observingSearchIndex{indexDelegate: idx}
	index = observed
	o = base
	o.Limit = 1
	page, err = backend.ItemSearchWithOptions(userid.NewContext(ctx, 1), o)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "new-244" {
		t.Fatalf("sparse: %+v %v", page, err)
	}
	if observed.calls < 4 || observed.maxIDs > 128 || observed.badScope {
		t.Fatalf("index batches: calls=%d max=%d badscope=%v", observed.calls, observed.maxIDs, observed.badScope)
	}
	observed.calls = 0
	_, err = backend.ItemSearchWithOptions(userid.NewContext(ctx, 1), microsub.ItemSearchOptions{Channel: "global", Query: "nomatchterm"})
	if err != nil || observed.calls < 4 || observed.badScope {
		t.Fatalf("global scan: %v calls=%d badscope=%v", err, observed.calls, observed.badScope)
	}
	cancelCtx, cancel := context.WithCancel(userid.NewContext(ctx, 1))
	observed.calls = 0
	observed.cancel = cancel
	_, err = backend.ItemSearchWithOptions(cancelCtx, o)
	if !errors.Is(err, context.Canceled) || observed.calls != 1 {
		t.Fatalf("cancellation: %v calls=%d", err, observed.calls)
	}
	index = idx
}

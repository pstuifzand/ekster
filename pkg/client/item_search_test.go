package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

func TestLegacyItemSearchFollowsServerPages(t *testing.T) {
	for _, total := range []int{73, 125} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				start := 0
				if raw := r.FormValue("after"); raw != "" {
					start, _ = strconv.Atoi(raw)
				}
				end := start + 20
				if end > total {
					end = total
				}
				page := microsub.Timeline{Items: []microsub.Item{}}
				for i := start; i < end; i++ {
					page.Items = append(page.Items, microsub.Item{ID: fmt.Sprint(i)})
				}
				if end < total {
					page.Paging.After = strconv.Itoa(end)
				}
				_ = json.NewEncoder(w).Encode(page)
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			c := &Client{MicrosubEndpoint: u}
			items, err := c.ItemSearch(context.Background(), "global", "word")
			if err != nil {
				t.Fatal(err)
			}
			want := total
			if want > 100 {
				want = 100
			}
			if len(items) != want {
				t.Fatalf("got %d want %d", len(items), want)
			}
			for i, item := range items {
				if item.ID != fmt.Sprint(i) {
					t.Fatalf("item %d = %s", i, item.ID)
				}
			}
			if requests < 4 {
				t.Fatalf("only %d pages requested", requests)
			}
		})
	}
}

func TestLegacyItemSearchRejectsRepeatingCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"paging":{"after":"same"}}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	_, err := (&Client{MicrosubEndpoint: u}).ItemSearch(context.Background(), "global", "word")
	if err == nil {
		t.Fatal("repeating cursor accepted")
	}
}

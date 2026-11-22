//go:build integration

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestChannelOrderPersistenceAndIsolation(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db,
		testChannel{UID: "notifications", Name: "Notifications"},
		testChannel{UID: "a", Name: "a"}, testChannel{UID: "b", Name: "b"},
		testChannel{UID: "c", Name: "c"}, testChannel{UID: "d", Name: "d"},
		testChannel{UID: "e", Name: "e"}, testChannel{UID: "f", Name: "f"},
		testChannel{UID: "g", Name: "g"}, testChannel{UID: "h", Name: "h"},
		testChannel{UID: "foreign", Name: "foreign", User: 2},
		testChannel{UID: "foreign-a", Name: "foreign-a", User: 2})
	if _, err := db.Exec(`INSERT INTO items (channel_id, uid, is_read)
		SELECT id, 'h-item', 0 FROM channels WHERE uid = 'h' AND user_id = 1`); err != nil {
		t.Fatal(err)
	}
	b := &memoryBackend{database: db}
	ctx := userid.NewContext(context.Background(), 1)
	other := userid.NewContext(context.Background(), 2)
	check := func(ctx context.Context, want ...string) {
		t.Helper()
		list, err := b.ChannelsGetList(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, c := range list {
			got = append(got, c.UID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	check(ctx, "notifications", "a", "b", "c", "d", "e", "f", "g", "h")
	if err := b.ChannelsOrder(ctx, []string{"d", "a", "c", "g"}); err != nil {
		t.Fatal(err)
	}
	check(ctx, "notifications", "d", "b", "a", "c", "e", "f", "g", "h")
	// A new backend sees persisted order; unread does not override it.
	b = &memoryBackend{database: db}
	check(ctx, "notifications", "d", "b", "a", "c", "e", "f", "g", "h")
	if err := b.ChannelsOrder(ctx, []string{"h", "notifications", "d"}); err != nil {
		t.Fatal(err)
	}
	check(ctx, "notifications", "h", "b", "a", "c", "e", "f", "g", "d")
	for _, input := range [][]string{{"a", "a"}, {"a", "foreign"}, {"missing"}} {
		if err := b.ChannelsOrder(ctx, input); !errors.Is(err, microsub.ErrInvalidChannelOrder) {
			t.Fatalf("%v: %v", input, err)
		}
	}
	check(ctx, "notifications", "h", "b", "a", "c", "e", "f", "g", "d")
	check(other, "foreign", "foreign-a")
	// Exercise the HTTP endpoint and client against the same persistent backend.
	handler, _ := server.NewMicrosubHandler(b)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
	defer ts.Close()
	endpoint, _ := url.Parse(ts.URL)
	c := &client.Client{MicrosubEndpoint: endpoint}
	if err := c.ChannelsOrder(ctx, []string{"d", "h"}); err != nil {
		t.Fatal(err)
	}
	check(ctx, "notifications", "d", "b", "a", "c", "e", "f", "g", "h")
	if _, err := db.Exec(`INSERT INTO channels(uid,name,user_id) VALUES ('new','new',1)`); err != nil {
		t.Fatal(err)
	}
	check(ctx, "notifications", "d", "b", "a", "c", "e", "f", "g", "h", "new")
}

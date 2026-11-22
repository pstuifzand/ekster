//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestChannelOrderPersistenceAndIsolation(t *testing.T) {
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
	migration, err := os.ReadFile("db/migrations/000019_add_channel_order.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TEMP TABLE channels (id int generated always as identity primary key, uid text not null, name text not null, user_id int not null)`,
		string(migration),
		`CREATE TEMP TABLE items (channel_id int, is_read int)`,
		`INSERT INTO channels(uid,name,user_id) VALUES ('notifications','Notifications',1),('a','a',1),('b','b',1),('c','c',1),('d','d',1),('e','e',1),('f','f',1),('g','g',1),('h','h',1),('foreign','foreign',2),('a','a',2)`,
		`INSERT INTO items(channel_id,is_read) SELECT id,0 FROM channels WHERE uid = 'h' AND user_id = 1`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
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
	check(other, "foreign", "a")
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

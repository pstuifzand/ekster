//go:build integration

package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestTimelineRemoveIsolationIntegration(t *testing.T) {
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
	for _, q := range []string{
		`CREATE TEMP TABLE channels (id int primary key, uid text, user_id int)`,
		`CREATE TEMP TABLE mutes (user_id int, channel_id int, url text)`,
		`CREATE TEMP TABLE items (id int primary key, channel_id int, uid text, is_read int, data jsonb)`,
		`INSERT INTO channels VALUES (1,'shared',1),(2,'shared',2),(3,'private',2),(4,'other',1)`,
		`INSERT INTO items (id,channel_id,uid,is_read) VALUES (1,1,'same',0),(2,2,'same',0),(3,3,'secret',0),(4,4,'other-item',0),(5,1,'keep',0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	backend := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	h, _ := server.NewMicrosubHandler(backend)
	call := func(user int, channel string, entries ...string) int {
		v := url.Values{"action": {"timeline"}, "method": {"remove"}, "channel": {channel}, "entry[]": entries}
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if user != 0 {
			r = r.WithContext(userid.NewContext(r.Context(), user))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if call(1, "private", "secret") != 404 || call(0, "shared", "same") == 200 || call(1, "missing", "same") != 404 {
		t.Fatal("invalid identity/channel accepted")
	}
	if call(1, "shared", "same", "secret", "other-item", "absent") != 200 {
		t.Fatal("remove failed")
	}
	rows, err := db.Query(`SELECT id FROM items ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(got, []int{2, 3, 4, 5}) {
		t.Fatalf("remaining IDs %v", got)
	}
	msg := <-backend.broker.Notifier
	ch, ok := msg.Object.(microsub.Channel)
	if !ok || msg.UserID == nil || *msg.UserID != 1 || ch.Unread.UnreadCount != 1 {
		t.Fatalf("notification %#v", msg)
	}
	if _, err := db.Exec(`INSERT INTO channels VALUES (5,'shared',1)`); err != nil {
		t.Fatal(err)
	}
	if call(1, "shared", "keep") != 404 {
		t.Fatal("ambiguous channel accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM items WHERE id=5`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("item lost: %d %v", count, err)
	}
	if err := backend.TimelineRemove(context.Background(), "shared", []string{"keep"}); err == nil {
		t.Fatal("missing identity accepted")
	}
}

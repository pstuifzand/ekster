//go:build integration

package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestReadStateUserIsolationIntegration(t *testing.T) {
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
		`CREATE TEMP TABLE channels (id int primary key, uid text, user_id int)`,
		`CREATE TEMP TABLE items (id int primary key, channel_id int, uid text, is_read int, data jsonb)`,
		`CREATE TEMP TABLE mutes (user_id int, channel_id int, url text)`,
		`INSERT INTO channels VALUES (1,'shared',1),(2,'shared',2),(3,'private',2),(4,'other',1)`,
		`INSERT INTO items VALUES (1,1,'same',1,NULL),(2,2,'same',1,NULL),(3,3,'secret',1,NULL),(4,4,'other-item',1,NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	backend := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 20)}}
	h, _ := server.NewMicrosubHandler(backend)
	call := func(user int, method, channel string, entries ...string) int {
		v := url.Values{"action": {"timeline"}, "method": {method}, "channel": {channel}, "entry[]": entries}
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if user != 0 {
			r = r.WithContext(userid.NewContext(r.Context(), user))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	check := func(want ...int) {
		t.Helper()
		rows, err := db.Query(`SELECT is_read FROM items ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		i := 0
		for rows.Next() {
			var got int
			if err := rows.Scan(&got); err != nil {
				t.Fatal(err)
			}
			if i >= len(want) || got != want[i] {
				t.Fatalf("item %d: got %d want %v", i+1, got, want)
			}
			i++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if i != len(want) {
			t.Fatal("wrong row count", i)
		}
	}
	notification := func(user, count int) {
		t.Helper()
		m := <-backend.broker.Notifier
		c, ok := m.Object.(microsub.Channel)
		if !ok || m.UserID == nil || *m.UserID != user || c.Unread.UnreadCount != count {
			t.Fatalf("notification %#v", m)
		}
	}
	if status := call(1, "mark_unread", "shared", "same", "secret", "other-item", "absent"); status != 200 {
		t.Fatal(status)
	}
	check(0, 1, 1, 1)
	notification(1, 1)
	if status := call(2, "mark_unread", "shared", "same"); status != 200 {
		t.Fatal(status)
	}
	check(0, 0, 1, 1)
	notification(2, 1)
	if status := call(1, "mark_read", "shared", "same", "secret", "other-item"); status != 200 {
		t.Fatal(status)
	}
	check(1, 0, 1, 1)
	notification(1, 0)
	if status := call(2, "mark_read", "shared", "same"); status != 200 {
		t.Fatal(status)
	}
	check(1, 1, 1, 1)
	notification(2, 0)
	for _, method := range []string{"mark_read", "mark_unread"} {
		if call(1, method, "private", "secret") == 200 {
			t.Fatal("foreign channel accepted", method)
		}
		if call(0, method, "shared", "same") == 200 {
			t.Fatal("missing identity accepted", method)
		}
		if call(1, method, "missing", "same") == 200 {
			t.Fatal("missing channel accepted", method)
		}
	}
	check(1, 1, 1, 1)
	// Ambiguous identities within one user's data fail closed, too.
	if _, err := db.Exec(`INSERT INTO channels VALUES (5,'shared',1)`); err != nil {
		t.Fatal(err)
	}
	if call(1, "mark_unread", "shared", "same") == 200 {
		t.Fatal("ambiguous channel accepted")
	}
	check(1, 1, 1, 1)
	if err := backend.MarkRead(context.Background(), "shared", []string{"same"}); err == nil {
		t.Fatal("missing backend identity accepted")
	}
}

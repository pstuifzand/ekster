//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestReadStateUserIsolationIntegration(t *testing.T) {
	db := integrationDB(t)
	// Channel UIDs and item UIDs are globally unique in the migrated schema, so
	// each user's channels and items need their own identifiers. The isolation
	// being tested is ownership, not name collision.
	seedChannels(t, db,
		testChannel{UID: "shared", Name: "Shared"},
		testChannel{UID: "shared-two", Name: "Shared two", User: 2},
		testChannel{UID: "private", Name: "Private", User: 2},
		testChannel{UID: "other", Name: "Other"})
	if _, err := db.Exec(`INSERT INTO items (id, channel_id, uid, is_read, data) OVERRIDING SYSTEM VALUE VALUES
		(1,1,'same',1,NULL),(2,2,'same-two',1,NULL),(3,3,'secret',1,NULL),(4,4,'other-item',1,NULL)`); err != nil {
		t.Fatal(err)
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
	if status := call(2, "mark_unread", "shared-two", "same-two"); status != 200 {
		t.Fatal(status)
	}
	check(0, 0, 1, 1)
	notification(2, 1)
	if status := call(1, "mark_read", "shared", "same", "secret", "other-item"); status != 200 {
		t.Fatal(status)
	}
	check(1, 0, 1, 1)
	notification(1, 0)
	if status := call(2, "mark_read", "shared-two", "same-two"); status != 200 {
		t.Fatal(status)
	}
	check(1, 1, 1, 1)
	notification(2, 0)
	for _, method := range []string{"mark_read", "mark_unread"} {
		if call(1, method, "private", "secret") == 200 {
			t.Fatal("foreign channel accepted", method)
		}
		if call(2, method, "shared", "same") == 200 {
			t.Fatal("foreign channel uid accepted", method)
		}
		if call(0, method, "shared", "same") == 200 {
			t.Fatal("missing identity accepted", method)
		}
		if call(1, method, "missing", "same") == 200 {
			t.Fatal("missing channel accepted", method)
		}
	}
	check(1, 1, 1, 1)
	// Channel UIDs are unique in the real schema, so a second channel with the
	// same uid for another user cannot exist. Prove the constraint instead of
	// testing an ambiguity the database rejects.
	if _, err := db.Exec(`INSERT INTO channels (uid, name, user_id) VALUES ('shared', 'Duplicate', 2)`); err == nil {
		t.Fatal("schema accepted a duplicate channel uid")
	}
	if call(1, "mark_read", "shared", "same") != 200 {
		t.Fatal("original channel stopped resolving")
	}
	check(1, 1, 1, 1)
	if err := backend.MarkRead(context.Background(), "shared", []string{"same"}); err == nil {
		t.Fatal("missing backend identity accepted")
	}
}

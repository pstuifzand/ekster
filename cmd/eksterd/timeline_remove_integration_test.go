//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestTimelineRemoveIsolationIntegration(t *testing.T) {
	db := integrationDB(t)
	// Channel and item UIDs are unique across the instance, so the two users'
	// channels and items carry distinct identifiers.
	seedChannels(t, db,
		testChannel{UID: "shared", Name: "Shared"},
		testChannel{UID: "shared-two", Name: "Shared two", User: 2},
		testChannel{UID: "private", Name: "Private", User: 2},
		testChannel{UID: "other", Name: "Other"})
	if _, err := db.Exec(`INSERT INTO items (id, channel_id, uid, is_read, data) OVERRIDING SYSTEM VALUE VALUES
		(1,1,'same',0,'{"type":"entry"}'),(2,2,'same-two',0,'{"type":"entry"}'),
		(3,3,'secret',0,'{"type":"entry"}'),(4,4,'other-item',0,'{"type":"entry"}'),
		(5,1,'keep',0,'{"type":"entry"}')`); err != nil {
		t.Fatal(err)
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
	if call(2, "shared", "same") != 404 {
		t.Fatal("foreign channel uid accepted")
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
	// A second channel with the same uid cannot exist in the real schema, so
	// there is no ambiguous-channel case to fail closed on. Assert the
	// constraint and that the item survives the rejected insert.
	if _, err := db.Exec(`INSERT INTO channels (uid, name, user_id) VALUES ('shared', 'Duplicate', 1)`); err == nil {
		t.Fatal("schema accepted a duplicate channel uid")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM items WHERE id=5`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("item lost: %d %v", count, err)
	}
	if err := backend.TimelineRemove(context.Background(), "shared", []string{"keep"}); err == nil {
		t.Fatal("missing identity accepted")
	}
}

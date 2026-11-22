//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/userid"
)

func TestTimelineRemoveClientIntegration(t *testing.T) {
	db := integrationDB(t)
	seedChannels(t, db, testChannel{UID: "one", Name: "One"}, testChannel{UID: "two", Name: "Two"})
	if _, err := db.Exec(`INSERT INTO items (id, channel_id, uid, data, created_at, is_read, published_at) OVERRIDING SYSTEM VALUE VALUES
		(1,1,'a','{"type":"entry"}',now(),1,now()),(2,1,'b','{"type":"entry"}',now(),1,now()),
		(3,2,'c','{"type":"entry"}',now(),1,now())`); err != nil {
		t.Fatal(err)
	}
	backend := &memoryBackend{database: db, broker: &sse.Broker{Notifier: make(chan sse.Message, 10)}}
	h, _ := server.NewMicrosubHandler(backend)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := &client.Client{MicrosubEndpoint: u}
	ctx := context.Background()
	if err := c.TimelineRemove(ctx, "one", []string{"a", "c", "missing"}); err != nil {
		t.Fatal(err)
	}
	one, err := c.TimelineGet(ctx, "", "", "one")
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Items) != 1 || one.Items[0].ID != "b" {
		t.Fatalf("one: %#v", one.Items)
	}
	two, err := c.TimelineGet(ctx, "", "", "two")
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Items) != 1 || two.Items[0].ID != "c" {
		t.Fatalf("two: %#v", two.Items)
	}
	if err := c.TimelineRemove(ctx, "missing", []string{"c"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing channel: %v", err)
	}
	msg := <-backend.broker.Notifier
	ch, ok := msg.Object.(microsub.Channel)
	if !ok || ch.UID != "one" || ch.Unread.UnreadCount != 0 {
		t.Fatalf("notification %#v", msg)
	}
}

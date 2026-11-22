package server

import (
	"context"
	"github.com/pstuifzand/ekster/pkg/client"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type unreadRecorder struct {
	NullBackend
	channel string
	entries []string
}

func (b *unreadRecorder) MarkUnread(_ context.Context, channel string, entries []string) error {
	b.channel = channel
	b.entries = append([]string(nil), entries...)
	return nil
}

func TestMarkUnreadHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, query, body string
		status            int
		entries           []string
	}{
		{"form", "", "action=timeline&method=mark_unread&channel=one&entry%5B%5D=a&entry%5B%5D=b", 200, []string{"a", "b"}},
		{"query", "?action=timeline&method=mark_unread&channel=one&entry=a", "", 200, []string{"a"}},
		{"indexed", "", "action=timeline&method=mark_unread&channel=one&entry%5B0%5D=a", 200, []string{"a"}},
		{"missing entries", "", "action=timeline&method=mark_unread&channel=one", 400, nil},
		{"empty entry", "", "action=timeline&method=mark_unread&channel=one&entry=", 400, nil},
		{"missing channel", "", "action=timeline&method=mark_unread&entry=a", 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &unreadRecorder{}
			h, _ := NewMicrosubHandler(b)
			r := httptest.NewRequest(http.MethodPost, "/microsub"+tc.query, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || !reflect.DeepEqual(b.entries, tc.entries) {
				t.Fatalf("status %d, channel %q, entries %v", w.Code, b.channel, b.entries)
			}
			if tc.status == 200 && b.channel != "one" {
				t.Fatal(b.channel)
			}
		})
	}
}

func TestReadStateRejectsConflictingMethods(t *testing.T) {
	for _, tc := range []struct{ query, body string }{
		{"mark_read", "method=mark_unread&channel=one&entry=a"},
		{"mark_read", "method=delete&channel=one&entry=a"},
		{"thread", "method=mark_unread&channel=one&entry=a"},
		{"mark_unread", "method=thread&channel=one&entry=a"},
	} {
		b := &unreadRecorder{}
		h, _ := NewMicrosubHandler(b)
		r := httptest.NewRequest(http.MethodPost, "/?action=timeline&method="+tc.query, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || b.entries != nil {
			t.Fatalf("query %q body %q: status %d, entries %v", tc.query, tc.body, w.Code, b.entries)
		}
	}
}

func TestMarkUnreadClient(t *testing.T) {
	b := &unreadRecorder{}
	h, _ := NewMicrosubHandler(b)
	s := httptest.NewServer(h)
	defer s.Close()
	u, _ := url.Parse(s.URL)
	c := &client.Client{MicrosubEndpoint: u}
	if err := c.MarkUnread(context.Background(), "one", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if b.channel != "one" || !reflect.DeepEqual(b.entries, []string{"a", "b"}) {
		t.Fatalf("%q %v", b.channel, b.entries)
	}
}

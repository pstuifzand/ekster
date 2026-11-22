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
	method  string
}

func (b *unreadRecorder) MarkUnread(_ context.Context, channel string, entries []string) error {
	b.channel = channel
	b.entries = append([]string(nil), entries...)
	b.method = "mark_unread"
	return nil
}

func (b *unreadRecorder) MarkRead(_ context.Context, channel string, entries []string) error {
	b.channel = channel
	b.entries = append([]string(nil), entries...)
	b.method = "mark_read"
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

func TestReadStateFormValuesPreferBody(t *testing.T) {
	for _, tc := range []struct{ query, body, want string }{
		{"mark_read", "method=mark_unread&channel=one&entry=a", "mark_unread"},
		{"mark_unread", "method=mark_read&channel=one&entry=a", "mark_read"},
		{"thread", "method=mark_unread&channel=one&entry=a", "mark_unread"},
		{"mark_unread", "channel=one&entry=a", "mark_unread"},
	} {
		b := &unreadRecorder{}
		h, _ := NewMicrosubHandler(b)
		r := httptest.NewRequest(http.MethodPost, "/?action=timeline&method="+tc.query, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || b.method != tc.want || b.channel != "one" || !reflect.DeepEqual(b.entries, []string{"a"}) {
			t.Fatalf("query %q body %q: status %d, method %q, channel %q, entries %v", tc.query, tc.body, w.Code, b.method, b.channel, b.entries)
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

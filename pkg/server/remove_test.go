package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type removeRecorder struct {
	NullBackend
	channel string
	entries []string
	method  string
}

func (b *removeRecorder) TimelineRemove(_ context.Context, channel string, entries []string) error {
	b.channel = channel
	b.entries = append([]string(nil), entries...)
	b.method = "remove"
	return nil
}

func TestRemoveHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, query, body string
		status            int
		entries           []string
	}{
		{"form", "", "action=timeline&method=remove&channel=one&entry%5B%5D=a&entry%5B%5D=b", 200, []string{"a", "b"}},
		{"body overrides query", "?action=other&method=mark_read&channel=wrong", "action=timeline&method=remove&channel=one&entry=a", 200, []string{"a"}},
		{"query", "?action=timeline&method=remove&channel=one&entry=a", "", 200, []string{"a"}},
		{"indexed", "", "action=timeline&method=remove&channel=one&entry%5B0%5D=a", 200, []string{"a"}},
		{"missing entries", "", "action=timeline&method=remove&channel=one", 400, nil},
		{"empty entry", "", "action=timeline&method=remove&channel=one&entry=", 400, nil},
		{"missing channel", "", "action=timeline&method=remove&entry=a", 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &removeRecorder{}
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

//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/sse"
)

func TestReadStateProductionRoutesIntegration(t *testing.T) {
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
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"me":"https://one.example/","scope":"read"}`)
	}))
	defer tokenServer.Close()
	for _, q := range []string{
		`CREATE TEMP TABLE users (id int, url text, token_endpoint text)`,
		`CREATE TEMP TABLE channels (id int, uid text, user_id int)`,
		`CREATE TEMP TABLE items (id int, channel_id int, uid text, is_read int)`,
		`INSERT INTO channels VALUES (1,'shared',1),(2,'shared',2),(3,'private',2)`,
		`INSERT INTO items VALUES (1,1,'item',1),(2,2,'item',1),(3,3,'secret',1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users VALUES (1,'https://one.example/',$1),(2,'https://two.example/',$1)`, tokenServer.URL); err != nil {
		t.Fatal(err)
	}
	pool := &redis.Pool{Dial: func() (redis.Conn, error) { return &fakeRedis{}, nil }}
	defer pool.Close()
	backend := &memoryBackend{database: db, pool: pool}
	unauth, broker := microsubRoute(backend, false)
	backend.broker = broker
	auth, _ := microsubRoute(backend, true)
	call := func(h http.Handler, user, method, channel, token string) int {
		t.Helper()
		v := url.Values{"action": {"timeline"}, "method": {method}, "channel": {channel}, "entry[]": {"item"}}
		req := httptest.NewRequest(http.MethodPost, "/microsub/"+user, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	check := func(id, want int) {
		t.Helper()
		var state int
		if err := db.QueryRow(`SELECT is_read FROM items WHERE id=$1`, id).Scan(&state); err != nil || state != want {
			t.Fatalf("item %d state=%d want=%d err=%v", id, state, want, err)
		}
	}
	for _, tc := range []struct {
		h                            http.Handler
		user, method, channel, token string
		status                       int
	}{
		{unauth, "1", "mark_unread", "shared", "", 200},
		{unauth, "1", "mark_read", "shared", "", 200},
		{unauth, "1", "mark_unread", "private", "", 404},
		{unauth, "1", "mark_read", "missing", "", 404},
		{unauth, "", "mark_read", "shared", "", 400},
		{auth, "1", "mark_unread", "shared", "", 403},
		{auth, "2", "mark_unread", "shared", "abc", 403},
		{auth, "1", "mark_unread", "private", "abc", 404},
		{auth, "1", "mark_unread", "shared", "abc", 200},
	} {
		if got := call(tc.h, tc.user, tc.method, tc.channel, tc.token); got != tc.status {
			t.Fatalf("%+v got %d", tc, got)
		}
		check(2, 1)
		check(3, 1)
	}
	check(1, 0)
	// The events endpoint must still authenticate and bind tokens to URL users.
	for _, path := range []string{"/microsub/1?action=events", "/microsub/2?action=events&access_token=abc"} {
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("events %s status %d", path, w.Code)
		}
	}
	// A stalled broker must not hold a committed write or its HTTP response.
	backend.broker = &sse.Broker{Notifier: make(chan sse.Message)}
	done := make(chan int, 1)
	go func() { done <- call(unauth, "1", "mark_read", "shared", "") }()
	select {
	case status := <-done:
		if status != 200 {
			t.Fatalf("blocked notifier status %d", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response blocked on notifier")
	}
	check(1, 1)
}

// An SSE response reaches the production route with the selected identity.
// Fail its second write so the long-lived handler can be joined deterministically.
type streamProbe struct {
	header  http.Header
	started chan struct{}
	writes  int
}

func (w *streamProbe) Header() http.Header { return w.header }
func (w *streamProbe) WriteHeader(int)     {}
func (w *streamProbe) Flush()              {}
func (w *streamProbe) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		close(w.started)
		return len(p), nil
	}
	return 0, fmt.Errorf("test disconnect")
}

func TestEventsAuthDisabledRoute(t *testing.T) {
	backend := &memoryBackend{}
	h, broker := microsubRoute(backend, false)
	backend.broker = broker
	w := &streamProbe{header: make(http.Header), started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/microsub/42?action=events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, req); close(done) }()
	select {
	case <-w.started:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream did not start")
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type %q", got)
	}
	id := 42
	broker.Notifier <- sse.Message{Event: "test", UserID: &id}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream did not finish")
	}
}

//go:build integration
// +build integration

/*
 *  Ekster is a microsub server
 *  Copyright (c) 2026 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/server"
	"github.com/pstuifzand/ekster/pkg/userid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threadTestBackend builds the backend against TEMP tables, so the queries
// under test run against the real column types and constraints without a
// production schema.
func threadTestBackend(t *testing.T) (*sql.DB, *memoryBackend) {
	t.Helper()

	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to run the thread integration test")
	}

	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// The temp tables live on one connection; a second pooled connection would
	// not see them.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	require.NoError(t, db.Ping())

	statements := []string{
		`CREATE TEMP TABLE channels (
			id int primary key generated always as identity,
			uid varchar(64) not null unique,
			name varchar(255) not null,
			user_id int not null default 1
		)`,
		`CREATE TEMP TABLE items (
			id int primary key generated always as identity,
			channel_id int not null,
			uid varchar(512) not null unique,
			data jsonb,
			is_read int default 0,
			created_at timestamptz default current_timestamp,
			updated_at timestamptz,
			published_at timestamptz
		)`,
		`INSERT INTO channels (uid, name, user_id) VALUES ('home', 'Home', 1), ('other', 'Other', 2)`,
	}
	for _, statement := range statements {
		_, err := db.Exec(statement)
		require.NoError(t, err, statement)
	}

	return db, &memoryBackend{database: db}
}

// seedThreadItem stores one entry the way the fetch path does: the item
// document in "data", the sort key in "published_at".
func seedThreadItem(t *testing.T, db *sql.DB, channelID int, uid, url, inReplyTo, published string) {
	t.Helper()

	item := microsub.Item{Type: "entry", UID: uid, URL: url}
	if inReplyTo != "" {
		item.InReplyTo = []string{inReplyTo}
	}
	publishedAt, err := time.Parse(time.RFC3339, published)
	require.NoError(t, err)

	data, err := json.Marshal(item)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO items (channel_id, uid, data, published_at) VALUES ($1, $2, $3, $4)`,
		channelID, uid, data, publishedAt)
	require.NoError(t, err, uid)
}

// A conversation is a root, the entry the client asked about, and everything
// that answers something already in the thread — but only from the user's own
// channels.
func TestThreadWalksParentsAndReplies(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 1, "root-uid", "https://ann.example/root", "", "2026-11-01T10:00:00Z")
	seedThreadItem(t, db, 1, "r1-uid", "https://bob.example/r1", "https://ann.example/root", "2026-11-02T10:00:00Z")
	seedThreadItem(t, db, 1, "r2-uid", "https://cat.example/r2", "https://bob.example/r1", "2026-11-03T10:00:00Z")
	// The same conversation, answered by an entry in another user's channel.
	seedThreadItem(t, db, 2, "f1-uid", "https://dan.example/f1", "https://ann.example/root", "2026-11-02T12:00:00Z")

	ctx := userid.NewContext(context.Background(), 1)

	thread, err := backend.ThreadGet(ctx, "", "", "https://bob.example/r1", 0)
	require.NoError(t, err)

	if !assert.Len(t, thread.Items, 3) {
		return
	}
	assert.Equal(t, "https://ann.example/root", thread.Items[0].URL, "the root comes first")
	assert.Equal(t, "https://bob.example/r1", thread.Items[1].URL)
	assert.Equal(t, "https://cat.example/r2", thread.Items[2].URL, "replies to replies are in the thread")

	for _, item := range thread.Items {
		assert.NotEqual(t, "https://dan.example/f1", item.URL, "another user's entry must not appear")
		assert.NotEmpty(t, item.ID, "an entry in the user's channels keeps its _id")
	}

	assert.Equal(t, "https://ann.example/root", thread.Thread.Root)
	assert.False(t, thread.Thread.Partial)
	assert.Equal(t, 0, thread.Thread.Missing)
}

func TestThreadByEntryMatchesByURL(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 1, "root-uid", "https://ann.example/root", "", "2026-11-01T10:00:00Z")
	seedThreadItem(t, db, 1, "r1-uid", "https://bob.example/r1", "https://ann.example/root", "2026-11-02T10:00:00Z")

	ctx := userid.NewContext(context.Background(), 1)

	byURL, err := backend.ThreadGet(ctx, "", "", "https://bob.example/r1", 0)
	require.NoError(t, err)
	byEntry, err := backend.ThreadGet(ctx, "home", "r1-uid", "", 0)
	require.NoError(t, err)

	assert.Equal(t, len(byURL.Items), len(byEntry.Items))
	assert.Equal(t, byURL.Thread, byEntry.Thread)
}

// A channel uid the caller owns but which does not contain the entry is still
// a hole in the scope, and another user's entry is not found at all.
func TestThreadIsScopedToTheUser(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 2, "f1-uid", "https://dan.example/f1", "", "2026-11-01T10:00:00Z")

	ctx := userid.NewContext(context.Background(), 1)

	_, err := backend.ThreadGet(ctx, "home", "f1-uid", "", 0)
	assert.True(t, errors.Is(err, microsub.ErrNotFound), "another user's entry is not found, got %v", err)

	_, err = backend.ThreadGet(ctx, "", "f1-uid", "", 0)
	assert.True(t, errors.Is(err, microsub.ErrNotFound), "and it is not found without a channel either, got %v", err)

	_, err = backend.ThreadGet(ctx, "", "", "https://dan.example/f1", 0)
	assert.True(t, errors.Is(err, microsub.ErrNotFound), "nor by url, got %v", err)
}

// An unknown URL that is not fetchable (here: a private address) is a 404, not
// a request to the network. The guard, not the internet, decides.
func TestThreadUnknownURLIsNotFound(t *testing.T) {
	_, backend := threadTestBackend(t)

	ctx := userid.NewContext(context.Background(), 1)
	_, err := backend.ThreadGet(ctx, "", "", "http://127.0.0.1/private", 0)
	assert.True(t, errors.Is(err, microsub.ErrNotFound), "got %v", err)
}

// An ancestor that is known to exist and cannot be read is what partial and
// missing report: the thread still shows the part it has.
func TestThreadReportsMissingAncestor(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 1, "orphan-uid", "https://eve.example/orphan", "http://127.0.0.1/private", "2026-11-01T10:00:00Z")

	ctx := userid.NewContext(context.Background(), 1)
	thread, err := backend.ThreadGet(ctx, "", "", "https://eve.example/orphan", 0)
	require.NoError(t, err)

	assert.True(t, thread.Thread.Partial, "the client has to be able to say the thread is incomplete")
	assert.Equal(t, 1, thread.Thread.Missing)
	assert.Len(t, thread.Items, 1, "the entry itself is still returned")
	assert.Equal(t, "https://eve.example/orphan", thread.Thread.Root)
}

// The depth cap is a cap, not an error: the thread stops there and says so.
func TestThreadDepthStopsTheWalk(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 1, "a-uid", "https://ann.example/a", "", "2026-11-01T10:00:00Z")
	seedThreadItem(t, db, 1, "b-uid", "https://bob.example/b", "https://ann.example/a", "2026-11-02T10:00:00Z")
	seedThreadItem(t, db, 1, "c-uid", "https://cat.example/c", "https://bob.example/b", "2026-11-03T10:00:00Z")

	ctx := userid.NewContext(context.Background(), 1)
	thread, err := backend.ThreadGet(ctx, "", "", "https://cat.example/c", 1)
	require.NoError(t, err)

	assert.Len(t, thread.Items, 2, "one hop up, then the walk stops")
	assert.Equal(t, "https://bob.example/b", thread.Thread.Root)
	assert.True(t, thread.Thread.Partial, "there is a parent above the root that was not walked")
}

// The whole path: a real HTTP request, the real handler, the real SQL. A test
// that stops at the backend cannot catch a routing mistake.
func TestThreadOverHTTP(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 1, "root-uid", "https://ann.example/root", "", "2026-11-01T10:00:00Z")
	seedThreadItem(t, db, 1, "r1-uid", "https://bob.example/r1", "https://ann.example/root", "2026-11-02T10:00:00Z")

	handler, _ := server.NewMicrosubHandler(backend)
	// Authentication puts the user in the context; this is that one step.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), 1)))
	}))
	defer ts.Close()

	res, err := http.Get(ts.URL + "/microsub?action=timeline&method=thread&url=" + url.QueryEscape("https://bob.example/r1"))
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got struct {
		Items  []microsub.Item     `json:"items"`
		Thread microsub.ThreadInfo `json:"thread"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))

	require.Len(t, got.Items, 2)
	assert.Equal(t, "https://ann.example/root", got.Items[0].URL)
	assert.Equal(t, "https://bob.example/r1", got.Items[1].URL)
	assert.Equal(t, []string{"https://ann.example/root"}, got.Items[1].InReplyTo)
	assert.Equal(t, "https://ann.example/root", got.Thread.Root)
	assert.False(t, got.Thread.Partial)
}

// seedMention stores an entry that refers to another post without answering it.
func seedMention(t *testing.T, db *sql.DB, channelID int, uid, url, mentionOf string) {
	t.Helper()

	item := microsub.Item{Type: "entry", UID: uid, URL: url, MentionOf: []string{mentionOf}}
	data, err := json.Marshal(item)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO items (channel_id, uid, data, published_at) VALUES ($1, $2, $3, now())`,
		channelID, uid, data)
	require.NoError(t, err, uid)
}

// An entry that mentions another post and answers nothing is a thread of one,
// which is not useful. The mentioned post comes back as a reference: it is not
// the conversation, so it is not in Items.
func TestThreadResolvesMentions(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 1, "mentioned-uid", "https://david.example/compass", "", "2026-11-09T10:00:00Z")
	seedMention(t, db, 1, "anchor-uid", "https://jeremy.example/compass", "https://david.example/compass")

	ctx := userid.NewContext(context.Background(), 1)
	thread, err := backend.ThreadGet(ctx, "", "anchor-uid", "", 0)
	require.NoError(t, err)

	require.Len(t, thread.Items, 1, "a mention is not an ancestor, so the conversation is one entry")
	assert.Equal(t, "https://jeremy.example/compass", thread.Items[0].URL)

	ref, ok := thread.Refs["https://david.example/compass"]
	require.True(t, ok, "the mentioned post must be a reference")
	assert.Equal(t, "mentioned-uid", ref.ID, "the indexed copy keeps its _id")
}

// A mention that cannot be resolved is skipped, not fatal: the thread is still
// true without it. A private address is the cheapest way to make the fetch fail
// without the network deciding the test.
func TestThreadMentionThatCannotBeResolvedIsSkipped(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedMention(t, db, 1, "anchor-uid", "https://jeremy.example/compass", "http://127.0.0.1/private")

	ctx := userid.NewContext(context.Background(), 1)
	thread, err := backend.ThreadGet(ctx, "", "anchor-uid", "", 0)
	require.NoError(t, err)

	assert.Len(t, thread.Items, 1)
	assert.Empty(t, thread.Refs)
}

// The reference lookup is scoped like everything else: another user's copy of
// the mentioned post is not the caller's to see.
func TestThreadMentionOfAnotherUsersEntryIsNotUsed(t *testing.T) {
	db, backend := threadTestBackend(t)

	seedThreadItem(t, db, 2, "theirs-uid", "http://127.0.0.1/private", "", "2026-11-09T10:00:00Z")
	seedMention(t, db, 1, "anchor-uid", "https://jeremy.example/compass", "http://127.0.0.1/private")

	ctx := userid.NewContext(context.Background(), 1)
	thread, err := backend.ThreadGet(ctx, "", "anchor-uid", "", 0)
	require.NoError(t, err)

	assert.Empty(t, thread.Refs, "a reference resolved from another user's channel would leak it")
}

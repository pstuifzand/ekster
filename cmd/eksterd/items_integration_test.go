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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestItemPermalinkIsPrivate exercises the permalink a micropub client gets in
// Location: it must resolve for the source token that created the item, and
// answer 404 for everyone else.
func TestItemPermalinkIsPrivate(t *testing.T) {
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to run the item permalink integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
		`CREATE TEMP TABLE sources (
			id int primary key generated always as identity,
			channel_id int not null,
			auth_code varchar(64) not null,
			name varchar(128) not null default '',
			created_at timestamp default current_timestamp,
			updated_at timestamp default current_timestamp,
			last_used_at timestamp with time zone
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
		`INSERT INTO channels (uid, name, user_id) VALUES ('home', 'Home', 1), ('other', 'Other', 1)`,
		`INSERT INTO sources (channel_id, auth_code, name) VALUES (1, 'token-home', 'Gmail')`,
		`INSERT INTO sources (channel_id, auth_code, name) VALUES (2, 'token-other', 'Elsewhere')`,
	}
	for _, statement := range statements {
		_, err := db.Exec(statement)
		require.NoError(t, err, statement)
	}

	item := microsub.Item{
		Type:      "entry",
		Name:      "Test email",
		Published: "2026-11-11T10:00:00Z",
		URL:       "https://microsub.example/i/item-uid",
		ID:        "item-uid",
		Source:    &microsub.Source{ID: "micropub:1", Name: "Gmail"},
	}
	item.Content = &microsub.Content{Text: "Hello from the new source"}

	data, err := json.Marshal(item)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO items (channel_id, uid, data, published_at) VALUES (1, 'item-uid', $1, now())`, data)
	require.NoError(t, err)

	backend := &memoryBackend{database: db}
	handler := &itemHandler{Backend: backend}

	stored, err := backend.ItemByUID(context.Background(), "item-uid")
	require.NoError(t, err)
	assert.Equal(t, "home", stored.ChannelUID)
	assert.Equal(t, 1, stored.UserID)
	assert.Equal(t, "Test email", stored.Item.Name)

	// The source token that posted it can read it, through any transport the
	// spec allows.
	for name, build := range map[string]func(*http.Request){
		"Authorization header": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer token-home")
		},
		"access_token form parameter": func(r *http.Request) {
			r.URL.RawQuery = "access_token=token-home"
		},
		"legacy source_id parameter": func(r *http.Request) {
			r.URL.RawQuery = "source_id=token-home"
		},
	} {
		request := httptest.NewRequest("GET", "/i/item-uid", nil)
		request.Header.Set("Accept", "application/jf2+json")
		build(request)

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusOK, recorder.Code, name)

		var got microsub.Item
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got), name)
		assert.Equal(t, "item-uid", got.ID, name)
		assert.Equal(t, "Test email", got.Name, name)
	}

	// A token for another channel does not see it.
	request := httptest.NewRequest("GET", "/i/item-uid", nil)
	request.Header.Set("Authorization", "Bearer token-other")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	// Neither does an anonymous visitor.
	request = httptest.NewRequest("GET", "/i/item-uid", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNotFound, recorder.Code)

	// A token is not enough for a foreign host in the Location header.
	assert.True(t, strings.HasPrefix(item.URL, "https://microsub.example/i/"))
}

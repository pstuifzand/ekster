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
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pstuifzand/ekster/pkg/userid"
)

// TestSourcesAreScopedToTheirUser exercises the SQL behind the sources page.
// A source is a bearer token, so a channel uid from another user must not be
// enough to create one, list it, or delete it.
func TestSourcesAreScopedToTheirUser(t *testing.T) {
	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		t.Skip("set DATABASE_TEST_URL to run the micropub sources integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}

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
		`INSERT INTO channels (uid, name, user_id) VALUES ('mine', 'Mine', 1), ('theirs', 'Theirs', 2)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	backend := &memoryBackend{database: db}
	mine := userid.NewContext(context.Background(), 1)
	theirs := userid.NewContext(context.Background(), 2)

	source, err := backend.SourceCreate(mine, "mine", "Gmail")
	if err != nil {
		t.Fatalf("SourceCreate: %v", err)
	}
	if source.Token == "" {
		t.Fatal("SourceCreate returned an empty token")
	}
	if len(source.Token) > 64 {
		t.Fatalf("token is %d characters, the auth_code column holds 64", len(source.Token))
	}

	// The token, not the row id, is what the source_id query parameter carries.
	request := httptest.NewRequest("POST", "/micropub?source_id="+source.Token, nil)
	target, err := resolveTarget(request, db, nil)
	if err != nil {
		t.Fatalf("getTargetFromAuthorization: %v", err)
	}
	if target.Channel != "mine" {
		t.Fatalf("token resolved to channel %q, want %q", target.Channel, "mine")
	}
	if target.SourceName != "Gmail" {
		t.Fatalf("token resolved to source name %q, want %q", target.SourceName, "Gmail")
	}

	backend.SourceTouch(source.ID)

	var lastUsed sql.NullTime
	if err := db.QueryRow(`SELECT last_used_at FROM sources WHERE id = $1`, source.ID).Scan(&lastUsed); err != nil {
		t.Fatalf("reading last_used_at: %v", err)
	}
	if !lastUsed.Valid {
		t.Fatal("SourceTouch did not set last_used_at")
	}

	if _, err := backend.SourceCreate(mine, "theirs", "Stolen"); err == nil {
		t.Fatal("created a source on another user's channel")
	}

	list, err := backend.SourcesGetList(theirs)
	if err != nil {
		t.Fatalf("SourcesGetList: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("another user sees %d sources, want 0", len(list))
	}

	if err := backend.SourceDelete(theirs, source.ID); err == nil {
		t.Fatal("deleted another user's source")
	}

	if err := backend.SourceDelete(mine, source.ID); err != nil {
		t.Fatalf("SourceDelete: %v", err)
	}

	// A deleted source must no longer authenticate.
	request = httptest.NewRequest("POST", "/micropub?source_id="+source.Token, nil)
	if _, err := resolveTarget(request, db, nil); err == nil {
		t.Fatal("a deleted source still resolves a channel")
	}
}

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
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/userid"
)

// TestItemSearchScopesToUser pins the search boundary: the bleve index holds
// every user's items, so a query must be limited to channels of the user in the
// context. Without that, "global" search in a multi-user instance returns other
// people's items.
func TestItemSearchScopesToUser(t *testing.T) {
	db := integrationDB(t)

	seedChannels(t, db, testChannel{UID: "home", Name: "Home"}, testChannel{UID: "theirs", Name: "Theirs", User: 2})
	if _, err := db.Exec(`INSERT INTO items (channel_id, uid, data, published_at)
		 VALUES (2, 'secret-item', '{"type":"entry","name":"Secret Buddha"}'::jsonb, now())`); err != nil {
		t.Fatal(err)
	}

	idx, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Index("secret-item", indexItem{Item: microsub.Item{Type: "entry", Name: "Secret Buddha"}, Channel: "theirs"}); err != nil {
		t.Fatal(err)
	}

	previous := index
	index = idx
	t.Cleanup(func() { index = previous })

	backend := &memoryBackend{database: db}
	userOne := userid.NewContext(context.Background(), 1)
	userTwo := userid.NewContext(context.Background(), 2)

	check := func(who string, ctx context.Context, channel string, want int) {
		t.Helper()
		items, err := backend.ItemSearch(ctx, channel, "Buddha")
		if err != nil {
			t.Fatalf("%s search %q: %v", who, channel, err)
		}
		if items == nil {
			t.Fatalf("%s search %q returned nil, want an empty slice", who, channel)
		}
		if len(items) != want {
			t.Fatalf("%s search %q returned %d items, want %d", who, channel, len(items), want)
		}
	}

	check("user 1", userOne, "global", 0)
	check("user 1", userOne, "theirs", 0)
	check("user 2", userTwo, "theirs", 1)
	check("user 2", userTwo, "global", 1)
}

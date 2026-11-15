/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
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
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

func TestQuerySearchReturnsMatchingIDs(t *testing.T) {
	idx, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	previous := index
	index = idx
	t.Cleanup(func() { index = previous })

	for id, channel := range map[string]string{"home-item": "home", "other-item": "elsewhere"} {
		if err := idx.Index(id, indexItem{Item: microsub.Item{Type: "entry", Name: "Sit Like a Buddha"}, Channel: channel}); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := querySearch("Buddha")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("querySearch returned %#v, want both items", ids)
	}

	missing, err := querySearch("nomatchterm")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("querySearch for a missing term returned %#v", missing)
	}
}

// A search index that stops answering must be skipped, not waited on once per
// item: 597 entries behind a wedged merge is hours of pauses that end with the
// refresh loop parked and the reader stale.
func TestIndexIsSkippedDuringItsCoolDown(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	previous := indexClock
	indexClock = func() time.Time { return now }
	t.Cleanup(func() {
		indexClock = previous
		indexBlockedUntil = time.Time{}
	})

	if indexBlocked() {
		t.Fatal("index is blocked before anything failed")
	}

	blockIndex()
	if !indexBlocked() {
		t.Fatal("index is not blocked after a timeout")
	}

	now = now.Add(indexCoolDown - time.Second)
	if !indexBlocked() {
		t.Fatal("the cool-down ended early")
	}

	now = now.Add(2 * time.Second)
	if indexBlocked() {
		t.Fatal("the index was never tried again after the cool-down")
	}
}

func TestIndexBatchWritesEveryEntryInOneCommit(t *testing.T) {
	idx, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	previous := index
	index = idx
	previousBlocked := indexBlockedUntil
	indexBlockedUntil = time.Time{}
	t.Cleanup(func() {
		index = previous
		indexBlockedUntil = previousBlocked
	})

	batch := &searchBatch{}
	for _, id := range []string{"a", "b", "c"} {
		batch.add("home", microsub.Item{ID: id})
	}
	if got := batch.len(); got != 3 {
		t.Fatalf("batch len = %d, want 3", got)
	}

	if err := indexBatch(batch.entries); err != nil {
		t.Fatalf("indexBatch: %v", err)
	}

	count, err := idx.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("DocCount = %d, want 3", count)
	}

	for _, id := range []string{"a", "b", "c"} {
		doc, err := idx.Document(id)
		if err != nil {
			t.Fatalf("item %s: %v", id, err)
		}
		if doc == nil {
			t.Fatalf("item %s missing from the index", id)
		}
	}
}

func TestIndexBatchIgnoresAnEmptyBatch(t *testing.T) {
	batch := &searchBatch{}
	if err := indexBatch(batch.entries); err != nil {
		t.Fatalf("empty batch returned %v", err)
	}
	if got := batch.len(); got != 0 {
		t.Fatalf("len = %d, want 0", got)
	}
}

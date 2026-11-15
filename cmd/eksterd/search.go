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
	"fmt"
	"log"
	"os"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

var index bleve.Index

// The search index is a nicety; fetching is the product. A bleve write that
// never returns must not take the refresh loop with it, which is exactly what
// happened: one feed with hundreds of entries left the loop parked for hours
// inside a segment merge. Writes are therefore bounded, and an index that
// misses its deadline is left alone for a while instead of costing every item
// the same wait.
const (
	indexTimeout  = 15 * time.Second
	indexCoolDown = 5 * time.Minute
)

var (
	indexBlockedUntil time.Time
	indexClock        = time.Now
)

func indexBlocked() bool {
	return indexClock().Before(indexBlockedUntil)
}

func blockIndex() {
	indexBlockedUntil = indexClock().Add(indexCoolDown)
}

func initSearch() error {
	if _, err := os.Stat("items.bleve"); os.IsNotExist(err) {
		mapping := bleve.NewIndexMapping()
		index, err = bleve.New("items.bleve", mapping)
		if err != nil {
			return err
		}
	} else {
		index, err = bleve.Open("items.bleve")
		if err != nil {
			return fmt.Errorf("while opening search index: %v", err)
		}
		return nil
	}
	return nil
}

type indexItem struct {
	microsub.Item
	Channel string `json:"channel"`
}

// searchEntry is one item waiting to be written to the index.
type searchEntry struct {
	id      string
	channel string
	item    microsub.Item
}

// searchBatch collects the items of one fetch so they reach bleve as a single
// write. Every Index call takes the writer lock, commits and leaves a segment
// behind; a Batch does that once for all of them. Ten items cost ten commits
// that way and one this way, which is most of why a refresh was 300ms.
type searchBatch struct {
	entries []searchEntry
}

func (b *searchBatch) add(channel string, item microsub.Item) {
	b.entries = append(b.entries, searchEntry{id: item.ID, channel: channel, item: item})
}

func (b *searchBatch) len() int {
	if b == nil {
		return 0
	}
	return len(b.entries)
}

// withIndexTimeout bounds a write, for the same reason single writes are
// bounded: a segment merge that never returns must not park the refresh loop.
func withIndexTimeout(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	timer := time.NewTimer(indexTimeout)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("while indexing item: %v", err)
		}
		return nil
	case <-timer.C:
		blockIndex()
		log.Printf("the search index did not answer within %s; indexing paused for %s",
			indexTimeout, indexCoolDown)
		return nil
	}
}

// indexBatch writes all entries in one commit.
func indexBatch(entries []searchEntry) error {
	if len(entries) == 0 || index == nil || indexBlocked() {
		return nil
	}

	batch := index.NewBatch()
	for _, e := range entries {
		if err := batch.Index(e.id, indexItem{e.item, e.channel}); err != nil {
			return fmt.Errorf("while building search batch: %v", err)
		}
	}

	return withIndexTimeout(func() error {
		return index.Batch(batch)
	})
}

// addToSearch indexes a single item, for callers outside a fetch.
func addToSearch(item microsub.Item, channel string) error {
	return indexBatch([]searchEntry{{id: item.ID, channel: channel, item: item}})
}

func getStringArray(fields map[string]interface{}, key string) []string {
	if value, e := fields[key]; e {
		if str, ok := value.([]string); ok {
			return str
		}
	}
	return []string{}
}

func getString(fields map[string]interface{}, key, def string) string {
	if value, e := fields[key]; e {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return def
}

// querySearch returns the uids of every item matching query. The result is not
// scoped to a user: the index spans the whole instance, so the caller resolves
// the uids against the database, where ownership lives.
func querySearch(query string) ([]string, error) {
	q := bleve.NewQueryStringQuery(query)

	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 100
	res, err := index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("while query %q: %v", query, err)
	}

	hits := res.Hits
	var ids []string
	for _, hit := range hits {
		itemIDStr := hit.ID
		ids = append(ids, itemIDStr)
	}

	return ids, nil
}

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

func addToSearch(item microsub.Item, channel string) error {
	if index == nil {
		return nil
	}
	if indexBlocked() {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- index.Index(item.ID, indexItem{item, channel})
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

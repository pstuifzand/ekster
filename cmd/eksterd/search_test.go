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
)

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

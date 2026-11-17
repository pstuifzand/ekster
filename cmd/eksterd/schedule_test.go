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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsSearchFeed(t *testing.T) {
	assert.True(t, isSearchFeed("https://hnrss.org/newest?points=50&q=postgres"))
	assert.True(t, isSearchFeed("https://www.hnrss.org/newcomments?q=marpa"))
	assert.False(t, isSearchFeed("https://p83.nl/"))
	assert.False(t, isSearchFeed("https://news.ycombinator.com/rss"))
	assert.False(t, isSearchFeed("not a url at all"))
}

// hnrss renders the query when it is asked (0.5s cached, 11s cold), so the five
// seconds that suit a static feed file expired on every cold query and the feed
// looked permanently broken while it was working.
func TestFetchBudgetGivesSearchFeedsMoreTime(t *testing.T) {
	assert.Equal(t, feedFetchTimeout, fetchBudget("https://p83.nl/"))
	assert.Equal(t, searchFeedTimeout, fetchBudget("https://hnrss.org/newest?q=postgres"))
	assert.Greater(t, searchFeedTimeout, feedFetchTimeout)
}

// The ladder is for feeds that publish rarely. A search never goes quiet, so it
// must not climb: applied to "postgres", two quiet fetches would put it at two
// hours and a week would put it out of sight.
func TestScheduleNextFetchKeepsSearchFeedsOnOneRung(t *testing.T) {
	b := &memoryBackend{}

	f := feed{ID: 1, URL: "https://hnrss.org/newest?points=50&q=postgres", Tier: maxTier}
	b.scheduleNextFetch(context.Background(), &f, false)

	assert.Equal(t, searchFeedTier, f.Tier)
	wait := time.Until(f.NextFetchAt)
	assert.InDelta(t, float64(1<<searchFeedTier)*float64(time.Minute), float64(wait), float64(5*time.Second))

	// A change must not knock it off the rung either.
	b.scheduleNextFetch(context.Background(), &f, true)
	assert.Equal(t, searchFeedTier, f.Tier)
}

func TestScheduleNextFetchStillClimbsForAPublication(t *testing.T) {
	b := &memoryBackend{}

	// Two unchanged fetches in a row buy one rung, as before.
	f := feed{ID: 2, URL: "https://p83.nl/", Tier: 3, Unmodified: climbAfter - 1}
	b.scheduleNextFetch(context.Background(), &f, false)

	assert.Equal(t, 4, f.Tier)
}

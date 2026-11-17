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

	"github.com/stretchr/testify/assert"
)

// The follower's box takes a name, an address or a feed URL. A bare term is the
// case that used to answer "no feeds matched", because both URL guesses name a
// host that does not exist.
func TestSearchFeedsForBareTerm(t *testing.T) {
	feeds := searchFeedsFor("postgres")

	if !assert.Len(t, feeds, 2, "a subject should offer a stories feed and a comments feed") {
		return
	}

	// url.Values.Encode sorts, so points precedes q. The filter is quoted in the
	// URL on purpose: it is the only place a reader can see why a quiet search
	// feed is quiet.
	assert.Equal(t, "https://hnrss.org/newest?points=50&q=postgres", feeds[0].URL)
	assert.Equal(t, "Hacker News: stories about \"postgres\"", feeds[0].Name)
	assert.Equal(t, "feed", feeds[0].Type)

	// A comment search has no points to filter on: the story's score is not the
	// comment's, and filtering would drop the reply that says the useful thing.
	assert.Equal(t, "https://hnrss.org/newcomments?q=postgres", feeds[1].URL)
	assert.Equal(t, "Hacker News: comments about \"postgres\"", feeds[1].Name)
}

func TestSearchFeedsForEscapesTheTerm(t *testing.T) {
	feeds := searchFeedsFor("home assistant")

	if !assert.Len(t, feeds, 2) {
		return
	}
	assert.Equal(t, "https://hnrss.org/newest?points=50&q=home+assistant", feeds[0].URL)
}

func TestSearchFeedsForAddresses(t *testing.T) {
	// An address is not a subject: the caller's own URL guesses apply, and the
	// suggested feed would be a search nobody asked for.
	for _, query := range []string{
		"",
		"   ",
		"p83.nl",
		"https://p83.nl/",
		"https://p83.nl/feed.xml",
		"p83.nl/feed",
		"user@example.com",
	} {
		assert.Empty(t, searchFeedsFor(query), "query %q is an address, not a search term", query)
	}
}

// The response must serialize as [] and never null: the MCP follow tool validates
// its output against {"type": "array"}.
func TestSearchFeedsForReturnsEmptySliceNotNil(t *testing.T) {
	feeds := searchFeedsFor("p83.nl")

	assert.NotNil(t, feeds)
	assert.Len(t, feeds, 0)
}

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

package jsonfeed

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	f, err := os.Open("testdata/feed.json")
	assert.NoError(t, err)

	feed, err := Parse(f)
	assert.NoError(t, err)

	assert.Equal(t, "https://jsonfeed.org/version/1", feed.Version)
	assert.Equal(t, "JSON Feed", feed.Title)
	assert.Equal(t, "https://www.jsonfeed.org/", feed.HomePageURL)
	assert.Equal(t, "https://www.jsonfeed.org/feed.json", feed.FeedURL)

	assert.Len(t, feed.Items, 2)
	assert.Equal(t, "http://jsonfeed.micro.blog/2020/08/07/json-feed-version.html", feed.Items[0].ID)
	assert.Equal(t, "http://jsonfeed.micro.blog/2017/05/17/announcing-json-feed.html", feed.Items[1].ID)
}

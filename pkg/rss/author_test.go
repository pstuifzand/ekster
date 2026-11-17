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

package rss

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseFixture(t *testing.T, name string) *Feed {
	t.Helper()

	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading %s", path)

	feed, err := Parse(data)
	require.NoError(t, err, "parsing %s", path)
	return feed
}

// A search feed from hnrss names the submitter in dc:creator. Without reading
// it, every entry looks like it was written by the feed itself.
func TestRSS2ItemAuthorFromDCCreator(t *testing.T) {
	feed := parseFixture(t, "rss_2.0_dc_creator")
	require.Len(t, feed.Items, 2)

	assert.Equal(t, "yonasb", feed.Items[0].Author)
	assert.Equal(t, "", feed.Items[1].Author, "an entry without a creator has no author of its own")
}

func TestAtomEntryAuthorName(t *testing.T) {
	feed := parseFixture(t, "atom_author")
	require.Len(t, feed.Items, 2)

	assert.Equal(t, "Brad Enslen", feed.Items[0].Author)
	assert.Equal(t, "", feed.Items[1].Author)
}

func TestItemAuthorSkipsBlankCandidates(t *testing.T) {
	assert.Equal(t, "", itemAuthor())
	assert.Equal(t, "", itemAuthor("  ", "\t"))
	assert.Equal(t, "second", itemAuthor("", "second", "third"))
	assert.Equal(t, "trimmed", itemAuthor(" trimmed "))
}

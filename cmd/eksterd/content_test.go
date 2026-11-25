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
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/stretchr/testify/assert"
)

// TestItemBodyRendersStoredHTML covers the shape a Micropub client posts as
// content[html]: it must reach the page as markup, not as visible tags.
func TestItemBodyRendersStoredHTML(t *testing.T) {
	got := string(itemBody(&microsub.Content{
		HTML: "<p>Hello <b>there</b></p>\n<p>Second paragraph</p>",
	}))

	assert.Contains(t, got, "<b>there</b>")
	assert.Contains(t, got, "<p>Second paragraph</p>")
	assert.NotContains(t, got, "&lt;")
}

// TestItemBodyRendersMarkdownText covers the shape Micropub clients post when
// they send Markdown in content without an html sibling.
func TestItemBodyRendersMarkdownText(t *testing.T) {
	got := string(itemBody(&microsub.Content{
		Text: "Hello **there**\n\nSecond paragraph\n\n* one\n* two",
	}))

	assert.Contains(t, got, "<strong>there</strong>")
	assert.Contains(t, got, "<p>Second paragraph</p>")
	assert.Contains(t, got, "<li>one</li>")
	assert.NotContains(t, got, "**")
}

// TestItemBodySanitizesUntrustedMarkup pins the security half: stored HTML can
// come from a feed or from any client with a source token, so script, event
// handlers and javascript: links must not survive to the signed-in reader.
func TestItemBodySanitizesUntrustedMarkup(t *testing.T) {
	got := string(itemBody(&microsub.Content{
		HTML: `<p onclick="steal()">Hi</p><script>alert(1)</script><a href="javascript:alert(2)">x</a>`,
	}))

	assert.NotContains(t, got, "<script")
	assert.NotContains(t, got, "onclick")
	assert.NotContains(t, got, "javascript:")
	assert.Contains(t, got, "Hi")
}

// TestItemBodyEscapesMarkupInText: a text-only item that happens to contain
// angle brackets must not smuggle tags through the Markdown renderer.
func TestItemBodyEscapesMarkupInText(t *testing.T) {
	got := string(itemBody(&microsub.Content{Text: `use <script>alert(1)</script> here`}))

	assert.NotContains(t, got, "<script")
	assert.Contains(t, got, "here")
}

func TestItemBodyWithoutContentIsEmpty(t *testing.T) {
	assert.Empty(t, string(itemBody(nil)))
	assert.Empty(t, string(itemBody(&microsub.Content{})))
}

// TestFillContentHTMLStoresRenderedMarkdown is the ingest side: a post that
// arrives as Markdown must leave with both forms, so a reader never has to know
// Markdown to show it.
func TestFillContentHTMLStoresRenderedMarkdown(t *testing.T) {
	content := &microsub.Content{Text: "Hello **there**\n\nSecond paragraph"}

	fillContentHTML(content)

	assert.Equal(t, "Hello **there**\n\nSecond paragraph", content.Text, "the source text is kept as sent")
	assert.Contains(t, content.HTML, "<strong>there</strong>")
	assert.Contains(t, content.HTML, "<p>Second paragraph</p>")
}

func TestFillContentHTMLKeepsClientHTML(t *testing.T) {
	content := &microsub.Content{Text: "Hello", HTML: "<p>Sent as <em>HTML</em></p>"}

	fillContentHTML(content)

	assert.Equal(t, "<p>Sent as <em>HTML</em></p>", content.HTML)
}

func TestFillContentHTMLWithoutText(t *testing.T) {
	content := &microsub.Content{}

	fillContentHTML(content)
	fillContentHTML(nil)

	assert.Empty(t, content.HTML)
}

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
	"bytes"
	"html/template"
	"log/slog"

	"github.com/microcosm-cc/bluemonday"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/yuin/goldmark"
)

// itemBodyPolicy is the allowlist an item's markup passes before the page
// renders it. Items come from feeds and from every client holding a source
// token, so stored HTML is untrusted: it may carry script, event handlers or
// javascript: links that would run in the signed-in reader's page.
var itemBodyPolicy = bluemonday.UGCPolicy()

// markdownToHTML renders plain item text the way a Micropub client meant it.
// Clients post Markdown in `content` without labelling it, and the Markdown
// renderer drops raw HTML by default, so nothing here reaches a browser before
// itemBody sanitizes it.
func markdownToHTML(text string) string {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(text), &buf); err != nil {
		slog.Error("could not render item text as markdown", "error", err)
		return ""
	}
	return buf.String()
}

// fillContentHTML stores the HTML form of the text a Micropub client posted.
// Clients send Markdown in `content` without labelling it, and a reader that is
// handed text alone has to parse Markdown itself to show the post. A stored
// item should always carry HTML whenever it has content, so the conversion
// happens once, on the way in. HTML the client sent is never overwritten.
func fillContentHTML(content *microsub.Content) {
	if content == nil || content.HTML != "" || content.Text == "" {
		return
	}

	content.HTML = markdownToHTML(content.Text)
}

// itemBody is the markup the item page shows. It uses stored HTML when the item
// has it and renders the text as Markdown otherwise, then sanitizes whichever
// it picked. Handing either to the template as a string escapes it: HTML posts
// showed their tags and Markdown showed its asterisks, both as literal text.
func itemBody(content *microsub.Content) template.HTML {
	if content == nil {
		return ""
	}

	body := content.HTML
	if body == "" {
		body = markdownToHTML(content.Text)
	}

	return template.HTML(itemBodyPolicy.Sanitize(body))
}

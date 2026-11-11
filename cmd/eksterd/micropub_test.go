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
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestItemURLRoundTrip(t *testing.T) {
	assert.Equal(t, "https://microsub.example/i/abc123", itemURL("https://microsub.example/", "abc123"))
	assert.Equal(t, "https://microsub.example/i/abc123", itemURL("https://microsub.example", "abc123"))

	for _, raw := range []string{
		"https://microsub.example/i/abc123",
		"https://microsub.example/i/abc123?x=1",
		"/i/abc123",
	} {
		assert.Equal(t, "abc123", uidFromItemURL(raw), raw)
	}

	for _, raw := range []string{
		"",
		"https://microsub.example/post/abc123",
		"/i/",
		"/i/abc/def",
	} {
		assert.Equal(t, "", uidFromItemURL(raw), raw)
	}
}

func TestParseFormItemKeepsProperties(t *testing.T) {
	form := url.Values{}
	form.Set("h", "note")
	form.Set("name", "A subject")
	form.Set("content", "Hello")
	form.Set("published", "2026-11-11T10:00:00Z")
	form.Set("summary", "Short")
	form.Add("category[]", "email")
	form.Add("category[]", "inbox")
	form.Add("photo[]", "https://example.com/p.jpg")
	form.Add("in-reply-to[]", "https://example.com/parent")
	form.Set("access_token", "secret-token")

	r := httptest.NewRequest("POST", "/micropub", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, r.ParseForm())

	item, err := parseIncomingItem(r)
	require.NoError(t, err)

	assert.Equal(t, "note", item.Type)
	assert.Equal(t, "A subject", item.Name)
	assert.Equal(t, "Short", item.Summary)
	assert.Equal(t, "2026-11-11T10:00:00Z", item.Published)
	require.NotNil(t, item.Content)
	assert.Equal(t, "Hello", item.Content.Text)
	assert.Equal(t, []string{"email", "inbox"}, item.Category)
	assert.Equal(t, []string{"https://example.com/p.jpg"}, item.Photo)
	assert.Equal(t, []string{"https://example.com/parent"}, item.InReplyTo)
}

func TestParseFormItemContentTypeWithCharset(t *testing.T) {
	form := url.Values{}
	form.Set("content", "Hello")

	r := httptest.NewRequest("POST", "/micropub", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	require.NoError(t, r.ParseForm())

	item, err := parseIncomingItem(r)
	require.NoError(t, err)
	assert.Equal(t, "entry", item.Type)
	require.NotNil(t, item.Content)
	assert.Equal(t, "Hello", item.Content.Text)
}

func TestParseFormItemHTMLContent(t *testing.T) {
	form := url.Values{}
	form.Set("content[html]", "<p>Hello <b>there</b></p>")

	r := httptest.NewRequest("POST", "/micropub", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, r.ParseForm())

	item, err := parseIncomingItem(r)
	require.NoError(t, err)
	require.NotNil(t, item.Content)
	assert.Equal(t, "<p>Hello <b>there</b></p>", item.Content.HTML)
}

func TestUIDFromItemURLRejectsForeignPaths(t *testing.T) {
	assert.Equal(t, "", uidFromItemURL("https://microsub.example/micropub?url=x"))
}

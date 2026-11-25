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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// onePixelPNG is a real 1x1 PNG, so sniffing has something to recognise.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0xda, 0x63, 0xfc, 0xcf, 0xc0, 0x50,
	0x0f, 0x00, 0x04, 0x85, 0x01, 0x80, 0x84, 0xa9, 0x8c, 0x21, 0x00, 0x00,
	0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// TestMediaContentTypeSniffsTheBytes: the label a client sends is a claim, and
// the file answers from ekster's own origin, so text/html must not be taken on
// trust even when it wears an image label.
func TestMediaContentTypeSniffsTheBytes(t *testing.T) {
	assert.Equal(t, "image/png", mediaContentType("text/html", onePixelPNG))
	assert.Equal(t, "text/html; charset=utf-8", mediaContentType("image/png", []byte("<html><body>hi</body></html>")))
}

// TestMediaContentTypeKeepsDeclaredForOpaqueBytes: audio and video often sniff
// as octet-stream, so a declared type is the only usable label left.
func TestMediaContentTypeKeepsDeclaredForOpaqueBytes(t *testing.T) {
	opaque := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}

	assert.Equal(t, "audio/mpeg", mediaContentType("audio/mpeg", opaque))
	assert.Equal(t, "application/octet-stream", mediaContentType("", opaque))
	assert.Equal(t, "application/octet-stream", mediaContentType("not a media type", opaque))
}

func TestMediaServedInline(t *testing.T) {
	for contentType, want := range map[string]bool{
		"image/png":            true,
		"image/png; charset=x": true,
		"audio/mpeg":           true,
		"video/mp4":            true,
		"image/svg+xml":        false,
		"text/html":            false,
		"application/pdf":      false,
	} {
		assert.Equal(t, want, mediaServedInline(contentType), contentType)
	}
}

func TestMediaDisposition(t *testing.T) {
	assert.Equal(t, `inline; filename="cat.png"`, mediaDisposition("image/png", "cat.png"))
	assert.Equal(t, `attachment; filename="page.html"`, mediaDisposition("text/html", "page.html"))
	assert.Equal(t, "attachment", mediaDisposition("application/pdf", ""))
}

// TestMediaFilenameKeepsAHeaderSafeBaseName: the suggested name is a client's
// string, and it ends up in a Content-Disposition header.
func TestMediaFilenameKeepsAHeaderSafeBaseName(t *testing.T) {
	assert.Equal(t, "passwd", mediaFilename("../../etc/passwd"))
	assert.Equal(t, "cat.png", mediaFilename(`C:\photos\cat.png`))
	assert.Equal(t, "say it out loud.png", mediaFilename(`say "it" out loud.png`))
	assert.Equal(t, "", mediaFilename(""))
	assert.Equal(t, "", mediaFilename("/"))
	assert.Len(t, []rune(mediaFilename(strings.Repeat("a", 200)+".png")), 120)
}

func TestValidMediaUID(t *testing.T) {
	assert.True(t, validMediaUID("0123456789abcdef0123456789abcdef"))
	assert.False(t, validMediaUID("0123456789ABCDEF0123456789abcdef"))
	assert.False(t, validMediaUID("0123456789abcdef0123456789abcde"))
	assert.False(t, validMediaUID("0123456789abcdef0123456789abcde/"))
}

func TestETagMatches(t *testing.T) {
	uid := "0123456789abcdef0123456789abcdef"
	etag := `"` + uid + `"`

	assert.True(t, etagMatches(etag, uid))
	assert.True(t, etagMatches("W/"+etag, uid))
	assert.True(t, etagMatches(`"other", `+etag, uid))
	assert.True(t, etagMatches("*", uid))
	assert.False(t, etagMatches(`"other"`, uid))
	assert.False(t, etagMatches("", uid))
}

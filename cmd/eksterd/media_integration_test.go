//go:build integration

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
	"database/sql"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mediaTestToken = "token-home"

// mediaTestBackend is a migrated database with one channel and one source
// token, which is everything the media endpoint authenticates against.
func mediaTestBackend(t *testing.T) *sql.DB {
	t.Helper()
	db := integrationDB(t)
	seedChannels(t, db, testChannel{UID: "home", Name: "Home"})
	_, err := db.Exec(`INSERT INTO sources (channel_id, auth_code, name) VALUES (1, $1, 'Micropub')`, mediaTestToken)
	require.NoError(t, err)
	return db
}

type mediaUpload struct {
	token       string
	filename    string
	contentType string
	data        []byte
	fields      map[string]string
}

func (u mediaUpload) request(t *testing.T) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for name, value := range u.fields {
		require.NoError(t, writer.WriteField(name, value))
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, u.filename))
	header.Set("Content-Type", u.contentType)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(u.data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/media", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if u.token != "" {
		request.Header.Set("Authorization", "Bearer "+u.token)
	}
	return request
}

func uploadMedia(t *testing.T, handler *mediaHandler, upload mediaUpload) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, upload.request(t))
	return recorder
}

// TestMediaUploadStoresAndServes walks the whole media endpoint contract: 201
// with a Location URL, the bytes in the database, and a public, cacheable,
// correct-typed answer at that URL.
func TestMediaUploadStoresAndServes(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := uploadMedia(t, handler, mediaUpload{
		token:       mediaTestToken,
		filename:    "cat.png",
		contentType: "image/png",
		data:        onePixelPNG,
	})
	require.Equal(t, http.StatusCreated, recorder.Code)

	location := recorder.Header().Get("Location")
	require.NotEmpty(t, location)
	assert.True(t, strings.HasPrefix(location, "http://ekster.example/media/"), location)

	parsed, err := url.Parse(location)
	require.NoError(t, err)
	require.True(t, validMediaUID(strings.TrimPrefix(parsed.Path, mediaPathPrefix)), parsed.Path)

	var contentType, filename string
	var data []byte
	require.NoError(t, handler.database.QueryRow(
		`SELECT content_type, filename, data FROM media WHERE uid = $1`,
		strings.TrimPrefix(parsed.Path, mediaPathPrefix)).Scan(&contentType, &filename, &data))
	assert.Equal(t, "image/png", contentType)
	assert.Equal(t, "cat.png", filename)
	assert.Equal(t, onePixelPNG, data)

	read := httptest.NewRequest(http.MethodGet, parsed.Path, nil)
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, read)

	assert.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, "image/png", got.Header().Get("Content-Type"))
	assert.Equal(t, `inline; filename="cat.png"`, got.Header().Get("Content-Disposition"))
	assert.Equal(t, "nosniff", got.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, got.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, onePixelPNG, got.Body.Bytes())

	// The URL is immutable, so a conditional request costs no body.
	conditional := httptest.NewRequest(http.MethodGet, parsed.Path, nil)
	conditional.Header.Set("If-None-Match", got.Header().Get("ETag"))
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, conditional)
	assert.Equal(t, http.StatusNotModified, notModified.Code)
	assert.Zero(t, notModified.Body.Len())

	head := httptest.NewRequest(http.MethodHead, parsed.Path, nil)
	headersOnly := httptest.NewRecorder()
	handler.ServeHTTP(headersOnly, head)
	assert.Equal(t, http.StatusOK, headersOnly.Code)
	assert.Zero(t, headersOnly.Body.Len())
	assert.Equal(t, fmt.Sprintf("%d", len(onePixelPNG)), headersOnly.Header().Get("Content-Length"))
}

// TestMediaUploadTakesTheTokenAsFormParameter: the spec requires both auth
// methods on every Micropub endpoint, and a multipart body carries the token as
// an ordinary field.
func TestMediaUploadTakesTheTokenAsFormParameter(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := uploadMedia(t, handler, mediaUpload{
		filename:    "note.png",
		contentType: "image/png",
		data:        onePixelPNG,
		fields:      map[string]string{"access_token": mediaTestToken},
	})

	assert.Equal(t, http.StatusCreated, recorder.Code)
}

func TestMediaUploadRejectsAnonymousCallers(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := uploadMedia(t, handler, mediaUpload{
		token:       "not-a-token",
		filename:    "cat.png",
		contentType: "image/png",
		data:        onePixelPNG,
	})

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "error")
}

func TestMediaUploadRejectsAnEmptyFile(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := uploadMedia(t, handler, mediaUpload{
		token:       mediaTestToken,
		filename:    "empty.png",
		contentType: "image/png",
	})

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestMediaUploadRejectsAnOversizedFile(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := uploadMedia(t, handler, mediaUpload{
		token:       mediaTestToken,
		filename:    "huge.bin",
		contentType: "application/octet-stream",
		data:        bytes.Repeat([]byte("x"), maxMediaSize+1),
	})

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

// TestMediaUploadServesMarkupAsAnAttachment: an uploaded document must not be
// rendered from ekster's origin, whatever the client called it.
func TestMediaUploadServesMarkupAsAnAttachment(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := uploadMedia(t, handler, mediaUpload{
		token:       mediaTestToken,
		filename:    "innocent.png",
		contentType: "image/png",
		data:        []byte("<html><body><script>alert(1)</script></body></html>"),
	})
	require.Equal(t, http.StatusCreated, recorder.Code)

	parsed, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)

	read := httptest.NewRequest(http.MethodGet, parsed.Path, nil)
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, read)

	assert.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, "text/html; charset=utf-8", got.Header().Get("Content-Type"))
	assert.True(t, strings.HasPrefix(got.Header().Get("Content-Disposition"), "attachment"))
}

func TestMediaReadRejectsUnknownUIDs(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	for _, path := range []string{
		"/media/0123456789abcdef0123456789abcdef", // well formed, never uploaded
		"/media/not-a-uid",
		"/media/",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, recorder.Code, path)
	}
}

func TestMediaEndpointAnswersOnlyPostAndReads(t *testing.T) {
	handler := &mediaHandler{database: mediaTestBackend(t), BaseURL: "http://ekster.example"}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/media", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/media/0123456789abcdef0123456789abcdef", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
}

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
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

const (
	// mediaEndpointPath is where clients upload; mediaPathPrefix is where the
	// stored file answers. Micropub discovers the former through q=config and
	// then uses the Location header of the upload to build the latter.
	mediaEndpointPath = "/media"
	mediaPathPrefix   = "/media/"

	// maxMediaSize caps one upload, and therefore one row: Postgres holds the
	// bytes. maxMediaMemory is how much of the multipart body is buffered in
	// memory before net/http spills parts to disk.
	maxMediaSize   = 10 << 20
	maxMediaMemory = 1 << 20
)

// mediaHandler is the Micropub media endpoint. A client uploads a file here and
// puts the returned URL in a later Micropub request as photo, video or audio.
//
// Reading is open: a post's <img src> cannot send a token, so the URL itself is
// the capability. That is why the uid is 128 random bits and never a counter —
// and why the endpoint is not listed anywhere.
type mediaHandler struct {
	database *sql.DB
	BaseURL  string
	pool     *redis.Pool
}

// storedMedia is one row of the media table, as the read side needs it.
type storedMedia struct {
	ContentType string
	Filename    string
	Size        int64
	Data        []byte
	CreatedAt   time.Time
}

func mediaEndpointURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + mediaEndpointPath
}

func mediaURL(baseURL, uid string) string {
	return mediaEndpointURL(baseURL) + "/" + uid
}

func (h *mediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == mediaEndpointPath:
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeMicropubError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleUpload(w, r)
	case strings.HasPrefix(r.URL.Path, mediaPathPrefix):
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
			writeMicropubError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleRead(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleUpload takes one multipart/form-data part named "file", authenticates
// the request the same way the Micropub endpoint does, and answers 201 with the
// URL of the stored file in Location.
func (h *mediaHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaSize+maxMediaMemory)
	if err := r.ParseMultipartForm(maxMediaMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeMicropubError(w, fmt.Sprintf("upload is larger than %d MiB", maxMediaSize>>20), http.StatusRequestEntityTooLarge)
			return
		}
		writeMicropubError(w, "expected a multipart/form-data upload", http.StatusBadRequest)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			if err := r.MultipartForm.RemoveAll(); err != nil {
				slog.ErrorContext(r.Context(), "could not remove the uploaded temp files", "error", err)
			}
		}
	}()

	// The multipart form is parsed, so a token sent as the access_token form
	// parameter is readable next to one sent in the Authorization header.
	target, err := resolveTarget(r, h.database, h.pool)
	if err != nil {
		slog.ErrorContext(r.Context(), "media upload could not be authorized", "error", err)
		writeMicropubError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if target.Channel == "" {
		writeMicropubError(w, "bad request, unknown channel", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeMicropubError(w, `expected one part named "file"`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxMediaSize+1))
	if err != nil {
		slog.ErrorContext(r.Context(), "could not read the uploaded file", "error", err)
		writeMicropubError(w, "could not read the upload", http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		writeMicropubError(w, "the upload is empty", http.StatusBadRequest)
		return
	}
	if len(data) > maxMediaSize {
		writeMicropubError(w, fmt.Sprintf("upload is larger than %d MiB", maxMediaSize>>20), http.StatusRequestEntityTooLarge)
		return
	}

	contentType := mediaContentType(header.Header.Get("Content-Type"), data)
	filename := mediaFilename(header.Filename)

	uid, err := h.store(r.Context(), target.Channel, contentType, filename, data)
	if err != nil {
		slog.ErrorContext(r.Context(), "could not store the uploaded file", "channel", target.Channel, "error", err)
		writeMicropubError(w, "could not store the upload", http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "media uploaded",
		"channel", target.Channel, "content_type", contentType, "size", len(data), "url", mediaURL(h.BaseURL, uid))

	w.Header().Set("Location", mediaURL(h.BaseURL, uid))
	w.WriteHeader(http.StatusCreated)
}

// handleRead serves a stored file. The URL is unguessable and the bytes never
// change, so the response may be cached forever.
func (h *mediaHandler) handleRead(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimPrefix(r.URL.Path, mediaPathPrefix)
	if !validMediaUID(uid) {
		http.NotFound(w, r)
		return
	}

	media, err := h.load(r.Context(), uid)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		slog.ErrorContext(r.Context(), "could not read media", "uid", uid, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("ETag", `"`+uid+`"`)
	if etagMatches(r.Header.Get("If-None-Match"), uid) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", media.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(media.Data)))
	w.Header().Set("Content-Disposition", mediaDisposition(media.ContentType, media.Filename))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// The stored type is sniffed rather than trusted, and nothing here may be
	// reinterpreted as a document: an upload answers as the file it is.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := w.Write(media.Data); err != nil {
		slog.ErrorContext(r.Context(), "could not write media", "uid", uid, "error", err)
	}
}

func (h *mediaHandler) store(ctx context.Context, channel, contentType, filename string, data []byte) (string, error) {
	var channelID int
	if err := h.database.QueryRowContext(ctx,
		`SELECT "id" FROM "channels" WHERE "uid" = $1`, channel).Scan(&channelID); err != nil {
		return "", fmt.Errorf("look up channel %q: %w", channel, err)
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	uid := hex.EncodeToString(raw[:])

	if _, err := h.database.ExecContext(ctx, `
INSERT INTO "media" ("uid", "channel_id", "content_type", "filename", "size", "data")
VALUES ($1, $2, $3, $4, $5, $6)
`, uid, channelID, contentType, filename, len(data), data); err != nil {
		return "", err
	}

	return uid, nil
}

func (h *mediaHandler) load(ctx context.Context, uid string) (storedMedia, error) {
	var media storedMedia
	err := h.database.QueryRowContext(ctx, `
SELECT "content_type", "filename", "size", "data", "created_at"
FROM "media" WHERE "uid" = $1
`, uid).Scan(&media.ContentType, &media.Filename, &media.Size, &media.Data, &media.CreatedAt)

	return media, err
}

// mediaContentType labels the upload by its bytes. The client's label is only a
// fallback for the types sniffing cannot recognise (audio and video often sniff
// as application/octet-stream): the file is served back from ekster's own
// origin, so a caller must not be able to have text/html labelled as an image.
func mediaContentType(declared string, data []byte) string {
	sniffed := http.DetectContentType(data)
	if sniffed != "application/octet-stream" {
		return sniffed
	}

	mediaType, _, err := mime.ParseMediaType(declared)
	if err != nil || mediaType == "" {
		return sniffed
	}
	return mime.FormatMediaType(mediaType, nil)
}

// mediaServedInline reports whether the file may render inside the page that
// loads it. SVG is a document that can carry script, so it stays an attachment
// like every non-media type; images, audio and video are what a post shows.
func mediaServedInline(contentType string) bool {
	mediaType := strings.ToLower(contentType)
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = strings.TrimSpace(mediaType[:idx])
	}
	if mediaType == "image/svg+xml" {
		return false
	}
	return strings.HasPrefix(mediaType, "image/") ||
		strings.HasPrefix(mediaType, "audio/") ||
		strings.HasPrefix(mediaType, "video/")
}

func mediaDisposition(contentType, filename string) string {
	disposition := "inline"
	if !mediaServedInline(contentType) {
		disposition = "attachment"
	}
	if filename == "" {
		return disposition
	}
	return fmt.Sprintf("%s; filename=%q", disposition, filename)
}

// mediaFilename keeps a suggested filename usable in a header: the base name
// only, no control characters and no quotes.
func mediaFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}

// validMediaUID is the shape store generates: 16 random bytes as lowercase hex.
func validMediaUID(uid string) bool {
	if len(uid) != 32 {
		return false
	}
	for _, r := range uid {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func etagMatches(header, uid string) bool {
	etag := `"` + uid + `"`
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || candidate == "W/"+etag {
			return true
		}
	}
	return false
}

// writeMicropubError answers in the Micropub error shape: JSON with an "error"
// message, so a client can show what went wrong.
func writeMicropubError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("could not write the error response", "error", err)
	}
}

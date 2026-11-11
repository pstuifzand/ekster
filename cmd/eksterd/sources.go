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
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/userid"
)

// micropubSource is a stored source: a token that lets an outside service post
// items into one channel. The token is the source's whole identity; there is no
// second secret, so it is generated once and only revocable by deleting the row.
type micropubSource struct {
	ID          int
	Name        string
	ChannelUID  string
	ChannelName string
	Token       string
	CreatedAt   time.Time
	LastUsedAt  sql.NullTime
}

// sourceView is a source formatted for the sources page.
type sourceView struct {
	ID             int
	Name           string
	ChannelUID     string
	ChannelName    string
	Token          string
	Endpoint       string
	LegacyEndpoint string
	Created        string
	LastUsed       string
	Used           bool
}

type sourcesPage struct {
	Session session

	Sources  []sourceView
	Channels []microsub.Channel
	Created  int
	Error    string
}

// newSourceToken makes a URL-safe token. The token travels in a query string
// pasted into another service, so it has to stay clean in a URL; 32 random
// bytes are far more than the 64 characters the column allows.
func newSourceToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SourcesGetList returns the micropub sources of the user in ctx.
func (b *memoryBackend) SourcesGetList(ctx context.Context) ([]micropubSource, error) {
	userID, _ := userid.FromContext(ctx)

	rows, err := b.database.Query(`
SELECT s."id", s."name", s."auth_code", s."created_at", s."last_used_at", c."uid", c."name"
FROM "sources" AS "s"
INNER JOIN "channels" AS "c" ON s."channel_id" = c."id"
WHERE c."user_id" = $1
ORDER BY s."created_at" DESC, s."id" DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []micropubSource
	for rows.Next() {
		var s micropubSource
		if err := rows.Scan(&s.ID, &s.Name, &s.Token, &s.CreatedAt, &s.LastUsedAt, &s.ChannelUID, &s.ChannelName); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}

	return sources, rows.Err()
}

// SourceCreate adds a source for a channel of the user in ctx. Scoping the
// insert through the channel row means a channel uid from another user cannot
// be used to plant a token.
func (b *memoryBackend) SourceCreate(ctx context.Context, channelUID, name string) (micropubSource, error) {
	userID, _ := userid.FromContext(ctx)

	token, err := newSourceToken()
	if err != nil {
		return micropubSource{}, fmt.Errorf("could not generate source token: %w", err)
	}

	row := b.database.QueryRow(`
INSERT INTO "sources" ("channel_id", "auth_code", "name")
SELECT c."id", $1, $2 FROM "channels" AS "c" WHERE c."uid" = $3 AND c."user_id" = $4
RETURNING "id", "created_at"
`, token, name, channelUID, userID)

	var s micropubSource
	if err := row.Scan(&s.ID, &s.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return micropubSource{}, fmt.Errorf("channel %q not found", channelUID)
		}
		return micropubSource{}, err
	}

	s.Name = name
	s.Token = token
	s.ChannelUID = channelUID

	return s, nil
}

// SourceDelete removes a source. It is the only way to revoke a token.
func (b *memoryBackend) SourceDelete(ctx context.Context, id int) error {
	userID, _ := userid.FromContext(ctx)

	result, err := b.database.Exec(`
DELETE FROM "sources"
WHERE "id" = $1
AND "channel_id" IN (SELECT "id" FROM "channels" WHERE "user_id" = $2)
`, id, userID)
	if err != nil {
		return err
	}

	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("source %d not found", id)
	}

	return nil
}

// SourceTouch records that a source just delivered an item.
func (b *memoryBackend) SourceTouch(id int) {
	if id == 0 {
		return
	}
	if _, err := b.database.Exec(`UPDATE "sources" SET "last_used_at" = now() WHERE "id" = $1`, id); err != nil {
		log.Printf("could not update last_used_at for source %d: %v", id, err)
	}
}

func (h *mainHandler) sourceViews(ctx context.Context, sources []micropubSource) []sourceView {
	baseURL := strings.TrimRight(h.BaseURL, "/")

	views := make([]sourceView, 0, len(sources))
	for _, s := range sources {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Source %d", s.ID)
		}

		view := sourceView{
			ID:          s.ID,
			Name:        name,
			ChannelUID:  s.ChannelUID,
			ChannelName: s.ChannelName,
			Token:       s.Token,
			Endpoint:    baseURL + "/micropub",
			// Sources created before the endpoint learned to read a bearer
			// token were handed this URL, so it keeps working.
			LegacyEndpoint: fmt.Sprintf("%s/micropub?source_id=%s", baseURL, url.QueryEscape(s.Token)),
			Created:        s.CreatedAt.UTC().Format("2006-01-02 15:04"),
		}
		if s.LastUsedAt.Valid {
			view.Used = true
			view.LastUsed = s.LastUsedAt.Time.UTC().Format("2006-01-02 15:04")
		}
		views = append(views, view)
	}

	return views
}

func (h *mainHandler) handleSourcesPage(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	_, sess, ok := h.pageSession(w, r, conn)
	if !ok {
		return
	}

	var page sourcesPage
	page.Session = sess

	sources, err := h.Backend.SourcesGetList(r.Context())
	if err != nil {
		log.Printf("ERROR: %s\n", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	page.Sources = h.sourceViews(r.Context(), sources)

	page.Channels, err = h.Backend.ChannelsGetList(r.Context())
	if err != nil {
		log.Printf("ERROR: %s\n", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if created := r.URL.Query().Get("created"); created != "" {
		page.Created, _ = strconv.Atoi(created)
	}
	page.Error = r.URL.Query().Get("error")

	if err := h.renderTemplate(w, "sources.html", page); err != nil {
		fmt.Fprintf(w, "ERROR: %s\n", err)
	}
}

func (h *mainHandler) handleSourceCreate(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	_, _, ok := h.pageSession(w, r, conn)
	if !ok {
		return
	}

	channelUID := r.FormValue("uid")
	name := strings.TrimSpace(r.FormValue("name"))
	if len(name) > 128 {
		name = name[:128]
	}

	if channelUID == "" {
		http.Redirect(w, r, "/settings/sources?error="+url.QueryEscape("Choose a channel for the source."), http.StatusFound)
		return
	}

	source, err := h.Backend.SourceCreate(r.Context(), channelUID, name)
	if err != nil {
		log.Printf("could not create source: %v", err)
		http.Redirect(w, r, "/settings/sources?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/settings/sources?created=%d", source.ID), http.StatusFound)
}

func (h *mainHandler) handleSourceDelete(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	_, _, ok := h.pageSession(w, r, conn)
	if !ok {
		return
	}

	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		http.Redirect(w, r, "/settings/sources?error="+url.QueryEscape("Unknown source."), http.StatusFound)
		return
	}

	if err := h.Backend.SourceDelete(r.Context(), id); err != nil {
		log.Printf("could not delete source %d: %v", id, err)
		http.Redirect(w, r, "/settings/sources?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	http.Redirect(w, r, "/settings/sources", http.StatusFound)
}

// pageSession loads the session and sends an anonymous visitor to the front
// page. Settings pages share this check.
func (h *mainHandler) pageSession(w http.ResponseWriter, r *http.Request, conn redis.Conn) (string, session, bool) {
	c, err := r.Cookie("session")
	if err == http.ErrNoCookie {
		http.Redirect(w, r, "/", http.StatusFound)
		return "", session{}, false
	}
	if err != nil {
		http.Error(w, "could not read cookie", http.StatusInternalServerError)
		return "", session{}, false
	}

	sess, err := loadSession(c.Value, conn)
	if err != nil {
		log.Printf("ERROR: %s\n", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return "", session{}, false
	}

	if !isLoggedIn(h.Backend, &sess) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, "Unauthorized")
		return "", session{}, false
	}

	return c.Value, sess, true
}

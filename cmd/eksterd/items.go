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
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/gomodule/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

// storedItem is an item with the channel facts needed to authorize reading it.
type storedItem struct {
	Item        microsub.Item
	ChannelUID  string
	ChannelName string
	UserID      int
}

// ItemByUID looks an item up by its public uid, which is the last segment of
// its permalink.
func (b *memoryBackend) UpdateMicropubItem(ctx context.Context, channel, uid string, item microsub.Item) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	result, err := b.database.ExecContext(ctx, `
UPDATE "items" AS i
SET "data" = $1
FROM "channels" AS c
WHERE i."channel_id" = c."id" AND c."uid" = $2 AND i."uid" = $3
`, string(data), channel, uid)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("item %q not found in channel %q", uid, channel)
	}
	return nil
}

func (b *memoryBackend) ItemByUID(ctx context.Context, uid string) (storedItem, error) {
	var si storedItem
	var raw []byte

	err := b.database.QueryRowContext(ctx, `
SELECT i."data", c."uid", c."name", c."user_id"
FROM "items" AS "i"
INNER JOIN "channels" AS "c" ON c."id" = i."channel_id"
WHERE i."uid" = $1
`, uid).Scan(&raw, &si.ChannelUID, &si.ChannelName, &si.UserID)
	if err == sql.ErrNoRows {
		return si, fmt.Errorf("item %q not found", uid)
	} else if err != nil {
		return si, err
	}

	if err := json.Unmarshal(raw, &si.Item); err != nil {
		return si, err
	}

	return si, nil
}

type itemHandler struct {
	Backend *memoryBackend
	pool    *redis.Pool
}

type itemPage struct {
	Item        microsub.Item
	Text        string
	ChannelUID  string
	ChannelName string
	SourceName  string
	AuthorName  string
}

func (h *itemHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uid := strings.TrimPrefix(r.URL.Path, itemPathPrefix)
	if uid == "" || strings.Contains(uid, "/") {
		http.NotFound(w, r)
		return
	}

	stored, err := h.Backend.ItemByUID(r.Context(), uid)
	if err != nil {
		log.Printf("item %q: %v", uid, err)
		http.NotFound(w, r)
		return
	}

	if !h.authorized(r, stored) {
		// A private item answers 404 to strangers: confirming that an item
		// exists but is private is itself a leak.
		http.NotFound(w, r)
		return
	}

	if wantsJSON(r) {
		writeJSON(w, stored.Item)
		return
	}

	page := itemPage{
		Item:        stored.Item,
		ChannelUID:  stored.ChannelUID,
		ChannelName: stored.ChannelName,
	}
	if stored.Item.Content != nil {
		page.Text = firstNonEmpty(stored.Item.Content.Text, stored.Item.Content.HTML)
	}
	if stored.Item.Source != nil {
		page.SourceName = stored.Item.Source.Name
	}
	if stored.Item.Author != nil && stored.Item.Author.Name != "" && stored.Item.Author.Name != page.SourceName {
		page.AuthorName = stored.Item.Author.Name
	}

	if err := renderPage(w, "item.html", page); err != nil {
		log.Printf("could not render item page: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// authorized accepts either the session of the user who owns the channel, or
// the token of a source posting into it.
func (h *itemHandler) authorized(r *http.Request, stored storedItem) bool {
	if h.pool != nil {
		if c, err := r.Cookie("session"); err == nil {
			conn := h.pool.Get()
			defer conn.Close()

			if sess, err := loadSession(c.Value, conn); err == nil {
				if isLoggedIn(h.Backend, &sess) && sess.UserID == stored.UserID {
					return true
				}
			}
		}
	}

	target, err := resolveTarget(r, h.Backend.database, h.pool)
	if err != nil {
		return false
	}

	return target.Channel != "" && target.Channel == stored.ChannelUID
}

func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/jf2+json") || strings.Contains(accept, "application/json")
}

func renderPage(w io.Writer, filename string, data interface{}) error {
	fsys, err := fs.Sub(templates, "templates")
	if err != nil {
		return err
	}
	t, err := template.ParseFS(fsys, "base.html", filename)
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, filename, data)
}

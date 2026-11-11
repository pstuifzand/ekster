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

package main

import (
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/pstuifzand/ekster/pkg/jf2"
	"github.com/pstuifzand/ekster/pkg/microsub"

	"github.com/gomodule/redigo/redis"
	"github.com/pkg/errors"
	willnorris "willnorris.com/go/microformats"
)

const itemPathPrefix = "/i/"

type micropubHandler struct {
	Backend *memoryBackend
	BaseURL string
	pool    *redis.Pool
}

func (h *micropubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		err := r.Body.Close()
		if err != nil {
			log.Printf("could not close request body: %v", err)
		}
	}()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.handleCreate(w, r)
	case http.MethodGet:
		h.handleQuery(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *micropubHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	target, err := resolveTarget(r, h.Backend.database, h.pool)
	if err != nil {
		log.Println(err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if target.Channel == "" {
		http.Error(w, "bad request, unknown channel", http.StatusBadRequest)
		return
	}

	item, err := parseIncomingItem(r)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if item.Published == "" {
		item.Published = time.Now().Format(time.RFC3339)
	}

	conn := h.pool.Get()
	defer conn.Close()

	newID, err := generateItemID(conn, target.Channel)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	item.Read = false
	item.ID = newID
	item.URL = itemURL(h.BaseURL, newID)
	item.Source = target.itemSource()

	log.Printf("Item published: %s (%s)", item.Published, item.URL)

	_, err = h.Backend.channelAddItemWithMatcher(target.Channel, *item)
	if err != nil {
		log.Printf("could not add item to channel %s: %v", target.Channel, err)
	}

	if err := h.Backend.updateChannelUnreadCount(target.Channel); err != nil {
		log.Printf("could not update channel unread content %s: %v", target.Channel, err)
	}

	h.Backend.SourceTouch(target.SourceID)

	// Micropub expects the URL of the created post in Location. The item is
	// private, so that URL answers only to the user or token that may read the
	// channel; the header is still the only address the client needs.
	w.Header().Set("Location", item.URL)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(map[string]string{"url": item.URL}); err != nil {
		log.Println(err)
	}
}

func (h *micropubHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	target, err := resolveTarget(r, h.Backend.database, h.pool)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.FormValue("q") {
	case "config":
		writeJSON(w, micropubConfig{
			PostTypes:   []micropubPostType{{Type: "entry", Name: "Note"}},
			Visibility:  []string{"private"},
			SyndicateTo: []string{},
		})
	case "source":
		h.handleSourceQuery(w, r, target)
	default:
		http.Error(w, "unsupported query", http.StatusBadRequest)
	}
}

func (h *micropubHandler) handleSourceQuery(w http.ResponseWriter, r *http.Request, target micropubTarget) {
	if target.Channel == "" {
		http.Error(w, "bad request, unknown channel", http.StatusBadRequest)
		return
	}

	if rawURL := r.FormValue("url"); rawURL != "" {
		uid := uidFromItemURL(rawURL)
		if uid == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		item, err := h.Backend.ItemByUID(r.Context(), uid)
		if err != nil || item.ChannelUID != target.Channel {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		writeJSON(w, item.Item)
		return
	}

	timeline, err := h.Backend.TimelineGet(r.Context(), "", "", target.Channel)
	if err != nil {
		log.Printf("q=source for %s: %v", target.Channel, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// A source token only sees what that source posted; an IndieAuth token is
	// not tied to one source, so it sees the whole channel timeline.
	items := make([]microsub.Item, 0, len(timeline.Items))
	for _, item := range timeline.Items {
		if target.SourceID != 0 {
			if item.Source == nil || item.Source.ID != fmt.Sprintf("micropub:%d", target.SourceID) {
				continue
			}
		}
		items = append(items, item)
	}

	writeJSON(w, items)
}

type micropubConfig struct {
	PostTypes   []micropubPostType `json:"post-types"`
	Visibility  []string           `json:"visibility"`
	SyndicateTo []string           `json:"syndicate-to"`
}

type micropubPostType struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println(err)
	}
}

func generateItemID(conn redis.Conn, channel string) (string, error) {
	id, err := redis.Int(conn.Do("INCR", fmt.Sprintf("source:%s:next_id", channel)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("source:%s:%d", channel, id)))), nil
}

// itemURL is the private permalink of an item. It is stable: updating or
// publishing an item keeps its uid, and the page can start redirecting to a
// public URL later without breaking anything that stored this one.
func itemURL(baseURL, uid string) string {
	return strings.TrimRight(baseURL, "/") + itemPathPrefix + uid
}

// uidFromItemURL accepts an absolute or relative item URL and returns its uid.
func uidFromItemURL(rawURL string) string {
	idx := strings.Index(rawURL, itemPathPrefix)
	if idx < 0 {
		return ""
	}

	uid := rawURL[idx+len(itemPathPrefix):]
	if i := strings.IndexAny(uid, "?#"); i >= 0 {
		uid = uid[:i]
	}
	if uid == "" || strings.Contains(uid, "/") {
		return ""
	}

	return uid
}

func parseIncomingItem(r *http.Request) (*microsub.Item, error) {
	contentType := r.Header.Get("content-type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	switch mediaType {
	case "application/jf2+json":
		var item microsub.Item
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			return nil, fmt.Errorf("could not decode request body as %q: %v", contentType, err)
		}
		return &item, nil
	case "application/json":
		var mfItem willnorris.Microformat
		if err := json.NewDecoder(r.Body).Decode(&mfItem); err != nil {
			return nil, fmt.Errorf("could not decode request body as %q: %v", contentType, err)
		}
		author := microsub.Card{}
		item, ok := jf2.SimplifyMicroformatItem(&mfItem, author)
		if !ok {
			return nil, fmt.Errorf("could not simplify microformat item to jf2")
		}
		return &item, nil
	case "application/x-www-form-urlencoded":
		return parseFormItem(r), nil
	}

	return nil, fmt.Errorf("content-type %q is not supported", contentType)
}

// parseFormItem reads the properties a form-encoded Micropub request can carry.
// Unrecognized properties are ignored, as the spec requires.
func parseFormItem(r *http.Request) *microsub.Item {
	item := &microsub.Item{Type: r.FormValue("h")}
	if item.Type == "" {
		item.Type = "entry"
	}

	item.Name = r.FormValue("name")
	item.Summary = r.FormValue("summary")

	text := firstNonEmpty(r.FormValue("content"), r.FormValue("content[text]"))
	html := r.FormValue("content[html]")
	if text != "" || html != "" {
		item.Content = &microsub.Content{Text: text, HTML: html}
	}

	item.Published = firstNonEmpty(r.FormValue("published"), r.FormValue("mp-published"))

	item.Category = append(r.Form["category[]"], r.Form["category"]...)
	item.Photo = append(r.Form["photo[]"], r.Form["photo"]...)
	item.InReplyTo = append(r.Form["in-reply-to[]"], r.Form["in-reply-to"]...)
	item.BookmarkOf = append(r.Form["bookmark-of[]"], r.Form["bookmark-of"]...)
	item.LikeOf = append(r.Form["like-of[]"], r.Form["like-of"]...)
	item.RepostOf = append(r.Form["repost-of[]"], r.Form["repost-of"]...)

	return item
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// micropubTarget is where an authorized micropub request lands: a source token
// names one stored source and its channel; an IndieAuth token names only a
// channel and has no source row.
type micropubTarget struct {
	SourceID   int
	SourceName string
	Channel    string
}

func (t micropubTarget) itemSource() *microsub.Source {
	if t.SourceID == 0 {
		return &microsub.Source{ID: "micropub", Name: "Micropub"}
	}

	name := t.SourceName
	if name == "" {
		name = fmt.Sprintf("Source %d", t.SourceID)
	}

	return &microsub.Source{
		ID:   fmt.Sprintf("micropub:%d", t.SourceID),
		Name: name,
	}
}

// resolveTarget finds the channel a request may post to. Micropub requires the
// token to be accepted both in the Authorization header and as the
// access_token form parameter; the source_id query parameter stays only for
// endpoints handed out before those existed.
func resolveTarget(r *http.Request, database *sql.DB, pool *redis.Pool) (micropubTarget, error) {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return resolveToken(strings.TrimSpace(header[len("Bearer "):]), database, pool)
	}

	if token := firstNonEmpty(r.FormValue("access_token"), r.URL.Query().Get("source_id")); token != "" {
		return resolveToken(token, database, pool)
	}

	return micropubTarget{}, fmt.Errorf("could not get channel from authorization")
}

func resolveToken(token string, database *sql.DB, pool *redis.Pool) (micropubTarget, error) {
	if token == "" {
		return micropubTarget{}, fmt.Errorf("empty token")
	}

	target, found, err := sourceTarget(database, token)
	if err != nil {
		return micropubTarget{}, err
	}
	if found {
		return target, nil
	}

	if pool == nil {
		return micropubTarget{}, fmt.Errorf("token is not a source token")
	}

	conn := pool.Get()
	defer conn.Close()

	channel, err := redis.String(conn.Do("HGET", "token:"+token, "channel"))
	if err != nil {
		return micropubTarget{}, errors.Wrap(err, "could not get channel for token")
	}

	return micropubTarget{Channel: channel}, nil
}

func sourceTarget(database *sql.DB, token string) (micropubTarget, bool, error) {
	var target micropubTarget

	row := database.QueryRow(`
SELECT s."id", s."name", c."uid"
FROM "sources" AS "s"
INNER JOIN "channels" AS "c" ON s."channel_id" = c."id"
WHERE s."auth_code" = $1
`, token)

	if err := row.Scan(&target.SourceID, &target.SourceName, &target.Channel); err == sql.ErrNoRows {
		return micropubTarget{}, false, nil
	} else if err != nil {
		return micropubTarget{}, false, err
	}

	return target, true, nil
}

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
	"net/http"
	"strings"
	"time"

	"github.com/pstuifzand/ekster/pkg/jf2"
	"github.com/pstuifzand/ekster/pkg/microsub"

	"github.com/gomodule/redigo/redis"
	"github.com/pkg/errors"
	"willnorris.com/go/microformats"
)

type micropubHandler struct {
	Backend *memoryBackend
	pool    *redis.Pool
}

func (h *micropubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		err := r.Body.Close()
		if err != nil {
			log.Printf("could not close request body: %v", err)
		}
	}()

	conn := h.pool.Get()
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Printf("could not close redis connection: %v", err)
		}
	}()

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost {
		source, err := getTargetFromAuthorization(r, conn, h.Backend.database)
		if err != nil {
			log.Println(err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// no channel is found
		if source.Channel == "" {
			http.Error(w, "bad request, unknown channel", http.StatusBadRequest)
			return
		}

		item, err := parseIncomingItem(r)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("Item published: %s", item.Published)
		if item.Published == "" {
			item.Published = time.Now().Format(time.RFC3339)
		}

		item.Read = false
		newID, err := generateItemID(conn, source.Channel)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		item.ID = newID

		item.Source = source.itemSource()

		_, err = h.Backend.channelAddItemWithMatcher(source.Channel, *item)
		if err != nil {
			log.Printf("could not add item to channel %s: %v", source.Channel, err)
		}

		err = h.Backend.updateChannelUnreadCount(source.Channel)
		if err != nil {
			log.Printf("could not update channel unread content %s: %v", source.Channel, err)
		}

		h.Backend.SourceTouch(source.SourceID)

		w.Header().Set("Content-Type", "application/json")

		if err = json.NewEncoder(w).Encode(map[string]string{"ok": "1"}); err != nil {
			log.Println(err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}

		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func generateItemID(conn redis.Conn, channel string) (string, error) {
	id, err := redis.Int(conn.Do("INCR", fmt.Sprintf("source:%s:next_id", channel)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("source:%s:%d", channel, id)))), nil
}

func parseIncomingItem(r *http.Request) (*microsub.Item, error) {
	contentType := r.Header.Get("content-type")

	if contentType == "application/jf2+json" {
		var item microsub.Item
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			return nil, fmt.Errorf("could not decode request body as %q: %v", contentType, err)
		}
		return &item, nil
	} else if contentType == "application/json" {
		var mfItem microformats.Microformat
		if err := json.NewDecoder(r.Body).Decode(&mfItem); err != nil {
			return nil, fmt.Errorf("could not decode request body as %q: %v", contentType, err)
		}
		author := microsub.Card{}
		item, ok := jf2.SimplifyMicroformatItem(&mfItem, author)
		if !ok {
			return nil, fmt.Errorf("could not simplify microformat item to jf2")
		}
		return &item, nil
	} else if contentType == "application/x-www-form-urlencoded" {
		// TODO: improve handling of form-urlencoded
		var item microsub.Item
		content := r.FormValue("content")
		name := r.FormValue("name")
		item.Type = "entry"
		item.Name = name
		item.Content = &microsub.Content{Text: content}
		item.Published = time.Now().Format(time.RFC3339)
		return &item, nil
	}

	return nil, fmt.Errorf("content-type %q is not supported", contentType)
}

// micropubTarget is where an authorized micropub POST lands: a source token
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

func getTargetFromAuthorization(r *http.Request, conn redis.Conn, database *sql.DB) (micropubTarget, error) {
	var target micropubTarget

	// backward compatible: the source_id query parameter carries the token, not
	// the row id. Existing sources are configured that way, so it stays.
	sourceID := r.URL.Query().Get("source_id")
	if sourceID != "" {
		row := database.QueryRow(`
SELECT s.id as source_id, s.name, c.uid
FROM "sources" AS "s"
INNER JOIN "channels" AS "c" ON s.channel_id = c.id
WHERE "auth_code" = $1
`, sourceID)

		if err := row.Scan(&target.SourceID, &target.SourceName, &target.Channel); err == sql.ErrNoRows {
			return target, errors.New("channel not found")
		} else if err != nil {
			return target, err
		}
		return target, nil
	}

	// full micropub with indieauth
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := authHeader[7:]
		channel, err := redis.String(conn.Do("HGET", "token:"+token, "channel"))
		if err != nil {
			return target, errors.Wrap(err, "could not get channel for token")
		}

		target.Channel = channel
		return target, nil
	}

	return target, fmt.Errorf("could not get channel from authorization")
}

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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"p83.nl/go/ekster/pkg/jf2"
	"p83.nl/go/ekster/pkg/microsub"

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
		var channel string

		channel, err = getChannelFromAuthorization(r, conn)
		if err != nil {
			log.Println(err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// no channel is found
		if channel == "" {
			http.Error(w, "bad request, unknown channel", http.StatusBadRequest)
			return
		}

		// TODO: We could try to fill the Source of the Item with something, but what?
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
		newID, err := generateItemID(conn, channel)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		item.ID = newID

		err = h.Backend.channelAddItemWithMatcher(channel, *item)
		if err != nil {
			log.Printf("could not add item to channel %s: %v", channel, err)
		}

		err = h.Backend.updateChannelUnreadCount(channel)
		if err != nil {
			log.Printf("could not update channel unread content %s: %v", channel, err)
		}

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

func getChannelFromAuthorization(r *http.Request, conn redis.Conn) (string, error) {
	// backward compatible
	sourceID := r.URL.Query().Get("source_id")
	if sourceID != "" {
		channel, err := redis.String(conn.Do("HGET", "sources", sourceID))
		if err != nil {
			return "", errors.Wrapf(err, "could not get channel for sourceID: %s", sourceID)
		}

		return channel, nil
	}

	// full micropub with indieauth
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := authHeader[7:]
		channel, err := redis.String(conn.Do("HGET", "token:"+token, "channel"))
		if err != nil {
			return "", errors.Wrap(err, "could not get channel for token")
		}

		return channel, nil
	}

	return "", fmt.Errorf("could not get channel from authorization")
}

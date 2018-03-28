/*
   Microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/microsub-server/microsub"
	"willnorris.com/go/microformats"
)

var (
	pool        *redis.Pool
	port        int
	redisServer = flag.String("redis", "redis:6379", "")
	entryRegex  = regexp.MustCompile("^entry\\[\\d+\\]$")
)

func init() {
	flag.IntVar(&port, "port", 80, "port for serving api")
}

type microsubHandler struct {
	Backend microsub.Microsub
	Redis   redis.Conn
}

func simplify(itemType string, item map[string][]interface{}) map[string]interface{} {
	feedItem := make(map[string]interface{})

	for k, v := range item {
		if k == "bookmark-of" || k == "like-of" || k == "repost-of" || k == "in-reply-to" {
			if value, ok := v[0].(*microformats.Microformat); ok {

				mType := value.Type[0][2:]
				m := simplify(mType, value.Properties)
				m["type"] = mType
				feedItem[k] = []interface{}{m}
			} else {
				feedItem[k] = v
			}
		} else if k == "content" {
			if content, ok := v[0].(map[string]interface{}); ok {
				if text, e := content["value"]; e {
					delete(content, "value")
					content["text"] = text
				}
				feedItem[k] = content
			}
		} else if k == "photo" {
			if itemType == "card" {
				if len(v) >= 1 {
					if value, ok := v[0].(string); ok {
						feedItem[k] = value
					}
				}
			} else {
				feedItem[k] = v
			}
		} else if k == "video" {
			feedItem[k] = v
		} else if k == "featured" {
			feedItem[k] = v
		} else if value, ok := v[0].(*microformats.Microformat); ok {
			mType := value.Type[0][2:]
			m := simplify(mType, value.Properties)
			m["type"] = mType
			feedItem[k] = m
		} else if value, ok := v[0].(string); ok {
			feedItem[k] = value
		} else if value, ok := v[0].(map[string]interface{}); ok {
			feedItem[k] = value
		} else if value, ok := v[0].([]interface{}); ok {
			feedItem[k] = value
		}
	}

	// Remove "name" when it's equals to "content[text]"
	if name, e := feedItem["name"]; e {
		if content, e2 := feedItem["content"]; e2 {
			if contentMap, ok := content.(map[string]interface{}); ok {
				if text, e3 := contentMap["text"]; e3 {
					if strings.TrimSpace(name.(string)) == strings.TrimSpace(text.(string)) {
						delete(feedItem, "name")
					}
				}
			}
		}
	}

	return feedItem
}

func simplifyMicroformat(item *microformats.Microformat) map[string]interface{} {
	itemType := item.Type[0][2:]
	newItem := simplify(itemType, item.Properties)
	newItem["type"] = itemType

	children := []map[string]interface{}{}

	if len(item.Children) > 0 {
		for _, c := range item.Children {
			child := simplifyMicroformat(c)
			if c, e := child["children"]; e {
				if ar, ok := c.([]map[string]interface{}); ok {
					children = append(children, ar...)
				}
				delete(child, "children")
			}
			children = append(children, child)
		}

		newItem["children"] = children
	}

	return newItem
}

func simplifyMicroformatData(md *microformats.Data) []map[string]interface{} {
	items := []map[string]interface{}{}
	for _, item := range md.Items {
		newItem := simplifyMicroformat(item)
		items = append(items, newItem)
		if c, e := newItem["children"]; e {
			if ar, ok := c.([]map[string]interface{}); ok {
				items = append(items, ar...)
			}
			delete(newItem, "children")
		}
	}
	return items
}

// TokenResponse is the information that we get back from the token endpoint of the user...
type TokenResponse struct {
	Me       string `json:"me"`
	ClientID string `json:"client_id"`
	Scope    string `json:"scope"`
	IssuedAt int64  `json:"issued_at"`
	Nonce    int64  `json:"nonce"`
}

var authHeaderRegex = regexp.MustCompile("^Bearer (.+)$")

func (h *microsubHandler) cachedCheckAuthToken(header string, r *TokenResponse) bool {
	log.Println("Cached checking Auth Token")

	tokens := authHeaderRegex.FindStringSubmatch(header)
	if len(tokens) != 2 {
		log.Println("No token found in the header")
		return false
	}
	key := fmt.Sprintf("token:%s", tokens[1])

	var err error

	values, err := redis.Values(h.Redis.Do("HGETALL", key))
	if err == nil && len(values) > 0 {
		if err = redis.ScanStruct(values, r); err == nil {
			return true
		}
	} else {
		log.Printf("Error while HGETALL %v\n", err)
	}

	authorized := h.checkAuthToken(header, r)

	if authorized {
		fmt.Printf("Token response: %#v\n", r)
		_, err = h.Redis.Do("HMSET", redis.Args{}.Add(key).AddFlat(r)...)
		if err != nil {
			log.Printf("Error while setting token: %v\n", err)
			return authorized
		}
		_, err = h.Redis.Do("EXPIRE", key, uint64(10*time.Minute))
		if err != nil {
			log.Printf("Error while setting expire on token: %v\n", err)
			log.Println("Deleting token")
			_, err = h.Redis.Do("DEL", key)
			if err != nil {
				log.Printf("Deleting token failed: %v", err)
			}
			return authorized
		}
	}

	return authorized
}

func (h *microsubHandler) checkAuthToken(header string, token *TokenResponse) bool {
	log.Println("Checking auth token")
	req, err := http.NewRequest("GET", "https://publog.stuifzandapp.com/authtoken", nil)
	if err != nil {
		log.Println(err)
		return false
	}

	req.Header.Add("Authorization", header)
	req.Header.Add("Accept", "application/json")

	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return false
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("HTTP StatusCode when verifying token: %d\n", res.StatusCode)
		return false
	}

	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&token)

	if err != nil {
		log.Printf("Error in json object: %v", err)
		return false
	}

	log.Println("Auth Token: Success")
	return true
}

func (h *microsubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var logger = log.New(os.Stdout, "logger: ", log.Lshortfile)
	h.Redis = redis.NewLoggingConn(pool.Get(), logger, "microsub")
	defer h.Redis.Close()

	r.ParseForm()
	log.Printf("%s %s\n", r.Method, r.URL)
	log.Println(r.URL.Query())
	log.Println(r.PostForm)
	authorization := r.Header.Get("Authorization")

	var token TokenResponse

	if !h.cachedCheckAuthToken(authorization, &token) {
		log.Printf("Token could not be validated")
		http.Error(w, "Can't validate token", 403)
		return
	}

	if token.Me != "https://publog.stuifzandapp.com/" {
		log.Printf("Missing \"me\" in token response: %#v\n", token)
		http.Error(w, "Wrong me", 403)
		return
	}

	if r.Method == http.MethodGet {
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			channels := h.Backend.ChannelsGetList()
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Channel{
				"channels": channels,
			})
		} else if action == "timeline" {
			timeline := h.Backend.TimelineGet(values.Get("after"), values.Get("before"), values.Get("channel"))
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.SetIndent("", "    ")
			jw.Encode(timeline)
		} else if action == "preview" {
			md, err := Fetch2(values.Get("url"))
			if err != nil {
				http.Error(w, "Failed parsing url", 500)
				return
			}

			results := simplifyMicroformatData(md)

			jw := json.NewEncoder(w)
			jw.SetIndent("", "    ")
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string]interface{}{
				"items":  results,
				"paging": microsub.Pagination{},
			})
		} else if action == "follow" {
			channel := values.Get("channel")
			following := h.Backend.FollowGetList(channel)
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Feed{
				"items": following,
			})
		} else {
			log.Printf("unknown action %s\n", action)
		}
		return
	} else if r.Method == http.MethodPost {
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			name := values.Get("name")
			method := values.Get("method")
			uid := values.Get("channel")
			if method == "delete" {
				h.Backend.ChannelsDelete(uid)
				w.Header().Add("Content-Type", "application/json")
				fmt.Fprintln(w, "[]")
				h.Backend.(Debug).Debug()
				return
			}

			jw := json.NewEncoder(w)
			if uid == "" {
				channel := h.Backend.ChannelsCreate(name)
				w.Header().Add("Content-Type", "application/json")
				jw.Encode(channel)
			} else {
				channel := h.Backend.ChannelsUpdate(uid, name)
				w.Header().Add("Content-Type", "application/json")
				jw.Encode(channel)
			}
			h.Backend.(Debug).Debug()
		} else if action == "follow" {
			uid := values.Get("channel")
			url := values.Get("url")

			feed := h.Backend.FollowURL(uid, url)
			w.Header().Add("Content-Type", "application/json")
			jw := json.NewEncoder(w)
			jw.Encode(feed)
		} else if action == "unfollow" {
			uid := values.Get("channel")
			url := values.Get("url")
			h.Backend.UnfollowURL(uid, url)
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else if action == "search" {
			query := values.Get("query")
			feeds := h.Backend.Search(query)
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Feed{
				"results": feeds,
			})
		} else if action == "timeline" || r.PostForm.Get("action") == "timeline" {
			method := values.Get("method")

			if method == "mark_read" || r.PostForm.Get("method") == "mark_read" {
				values = r.PostForm
				channel := values.Get("channel")
				if uids, e := values["entry"]; e {
					h.Backend.MarkRead(channel, uids)
				} else if uids, e := values["entry[]"]; e {
					h.Backend.MarkRead(channel, uids)
				} else {
					uids := []string{}
					for k, v := range values {
						if entryRegex.MatchString(k) {
							uids = append(uids, v...)
						}
					}
					h.Backend.MarkRead(channel, uids)
				}
			} else {
				log.Printf("unknown method in timeline %s\n", method)
			}
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else {
			log.Printf("unknown action %s\n", action)
		}

		return
	}
	return
}

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

func main() {
	flag.Parse()

	var logger = log.New(os.Stdout, "logger: ", log.Lshortfile)

	createBackend := false
	args := flag.Args()

	if len(args) >= 1 {
		if args[0] == "new" {
			createBackend = true
		}
	}

	pool = newPool(*redisServer)

	conn := redis.NewLoggingConn(pool.Get(), logger, "microsub")
	defer conn.Close()

	var backend microsub.Microsub

	if createBackend {
		backend = createMemoryBackend()
	} else {
		backend = loadMemoryBackend(conn)
	}

	http.Handle("/microsub", &microsubHandler{backend, nil})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

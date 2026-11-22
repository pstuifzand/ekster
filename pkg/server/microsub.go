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

/*
Package server contains the microsub server itself. It implements http.Handler.
It follows the spec at https://indieweb.org/Microsub-spec.
*/
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/sse"
)

var (
	entryRegex = regexp.MustCompile(`^entry\[\d+\]$`)
)

// previewStatus separates a URL that cannot be a feed from a fetch that went
// wrong. A preview carries a URL the person typed, so "this is not a feed" is
// their input talking, not a broken server: it answers 400 with the reason.
// Everything else — DNS, timeouts, a 5xx upstream — stays a 500.
func previewStatus(err error) int {
	if errors.Is(err, microsub.ErrNotAFeed) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// Constants used for the responses
const (
	OutputContentType = "application/json; charset=utf-8"
)

type microsubHandler struct {
	backend microsub.Microsub
	Broker  *sse.Broker
}

func respondJSON(w http.ResponseWriter, value interface{}) {
	jw := json.NewEncoder(w)
	jw.SetIndent("", "    ")
	jw.SetEscapeHTML(false)
	w.Header().Add("Content-Type", OutputContentType)
	err := jw.Encode(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// NewMicrosubHandler is the main entry point for the microsub server
// It returns a handler for HTTP and a broker that will send events.
func NewMicrosubHandler(backend microsub.Microsub) (http.Handler, *sse.Broker) {
	broker := sse.NewBroker()
	return &microsubHandler{backend, broker}, broker
}

// Methods required by http.Handler
func (h *microsubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	if r.Method == http.MethodOptions {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Methods", "GET, POST")
		w.Header().Add("Access-Control-Allow-Headers", "Authorization, Cache-Control, Last-Event-ID")
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			channels, err := h.backend.ChannelsGetList(r.Context())
			if err != nil {
				slog.ErrorContext(r.Context(), "could not list channels", "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			respondJSON(w, map[string][]microsub.Channel{
				"channels": channels,
			})
		} else if action == "timeline" {
			// thread is a read about a post, so it belongs on the GET side
			// even though mark_read and friends are POSTs.
			if values.Get("method") == "thread" {
				h.handleThread(w, r, values)
				return
			}
			var timeline microsub.Timeline
			var err error
			if raw, present := values["limit"]; present {
				if len(raw) != 1 {
					http.Error(w, "limit must be a single positive integer", http.StatusBadRequest)
					return
				}
				limit, parseErr := strconv.Atoi(raw[0])
				if parseErr != nil || limit <= 0 {
					http.Error(w, "limit must be a single positive integer", http.StatusBadRequest)
					return
				}
				timeline, err = h.backend.TimelineGetWithLimit(r.Context(), values.Get("before"), values.Get("after"), values.Get("channel"), limit)
			} else {
				timeline, err = h.backend.TimelineGet(r.Context(), values.Get("before"), values.Get("after"), values.Get("channel"))
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "could not read the timeline", "channel", values.Get("channel"), "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			respondJSON(w, timeline)
		} else if action == "preview" {
			timeline, err := h.backend.PreviewURL(r.Context(), values.Get("url"))
			if err != nil {
				slog.ErrorContext(r.Context(), "could not preview url", "url", values.Get("url"), "error", err)
				http.Error(w, err.Error(), previewStatus(err))
				return
			}
			respondJSON(w, timeline)
		} else if action == "follow" {
			channel := values.Get("channel")
			following, err := h.backend.FollowGetList(r.Context(), channel)
			if err != nil {
				slog.ErrorContext(r.Context(), "could not list the followed feeds", "channel", channel, "error", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			respondJSON(w, map[string][]microsub.Feed{
				"items": following,
			})
		} else if action == "events" {
			events, err := h.backend.Events(r.Context())
			if err != nil {
				slog.ErrorContext(r.Context(), "could not start the event stream", "error", err)
				http.Error(w, "could not start sse connection", http.StatusInternalServerError)
				return
			}

			// Remove this client from the map of connected clients
			// when this handler exits.
			defer func() {
				h.Broker.CloseClient(events)
			}()

			// Listen to connection close and un-register messageChan
			notify := r.Context().Done()

			go func() {
				<-notify
				h.Broker.CloseClient(events)
			}()

			err = sse.WriteMessages(w, events)
			if err != nil {
				slog.ErrorContext(r.Context(), "could not write the event stream", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		} else {
			http.Error(w, fmt.Sprintf("unknown action %s", action), http.StatusBadRequest)
			return
		}
		return
	} else if r.Method == http.MethodPost {
		w.Header().Add("Access-Control-Allow-Origin", "*")

		values := r.Form
		action := values.Get("action")
		if action == "channels" {
			name := values.Get("name")
			method := values.Get("method")
			uid := values.Get("channel")
			if method == "order" {
				if err := h.backend.ChannelsOrder(r.Context(), values["channels[]"]); err != nil {
					if errors.Is(err, microsub.ErrInvalidChannelOrder) {
						http.Error(w, err.Error(), http.StatusBadRequest)
					} else {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
					return
				}
				respondJSON(w, map[string]interface{}{})
				return
			}
			if method == "delete" {
				err := h.backend.ChannelsDelete(r.Context(), uid)
				if err != nil {
					slog.ErrorContext(r.Context(), "could not delete channel", "channel", uid, "error", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				respondJSON(w, []string{})
				return
			}

			if uid == "" {
				channel, err := h.backend.ChannelsCreate(r.Context(), name)
				if err != nil {
					slog.ErrorContext(r.Context(), "could not create channel", "name", name, "error", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				respondJSON(w, channel)
			} else if name != "" {
				channel, err := h.backend.ChannelsUpdate(r.Context(), uid, name)
				if err != nil {
					slog.ErrorContext(r.Context(), "could not update channel", "channel", uid, "error", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				respondJSON(w, channel)
			}
		} else if action == "follow" {
			uid := values.Get("channel")
			url := values.Get("url")
			// h.HubIncomingBackend.CreateFeed(url, uid)
			feed, err := h.backend.FollowURL(r.Context(), uid, url)
			if err != nil {
				if errors.Is(err, microsub.ErrFeedAlreadyFollowed) {
					http.Error(w, err.Error(), http.StatusConflict)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			respondJSON(w, feed)
		} else if action == "unfollow" {
			uid := values.Get("channel")
			url := values.Get("url")
			err := h.backend.UnfollowURL(r.Context(), uid, url)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			respondJSON(w, []string{})
		} else if action == "preview" {
			timeline, err := h.backend.PreviewURL(r.Context(), values.Get("url"))
			if err != nil {
				http.Error(w, err.Error(), previewStatus(err))
				return
			}
			respondJSON(w, timeline)
		} else if action == "search" {
			query := values.Get("query")
			channel := values.Get("channel")
			if channel == "" {
				feeds, err := h.backend.Search(r.Context(), query)
				if err != nil {
					respondJSON(w, map[string]interface{}{
						"query": query,
						"error": err.Error(),
					})
					return
				}
				respondJSON(w, map[string][]microsub.Feed{
					"results": feeds,
				})
			} else {
				items, err := h.backend.ItemSearch(r.Context(), channel, query)
				if err != nil {
					respondJSON(w, map[string]interface{}{
						"query": query,
						"error": err.Error(),
					})
					return
				}
				slog.DebugContext(r.Context(), "search", "query", query, "channel", channel, "results", len(items))
				respondJSON(w, map[string]interface{}{
					"query": query,
					"items": items,
				})
			}
		} else if action == "timeline" || r.PostForm.Get("action") == "timeline" {
			method := values.Get("method")

			// Reject ambiguous requests before dispatch: ParseForm merges body and
			// query values, and silently preferring either can invert a read action.
			methods := r.Form["method"]
			readState := false
			for _, m := range methods {
				if m == "mark_read" || m == "mark_unread" {
					readState = true
				}
			}
			if readState {
				for _, m := range methods {
					if m != methods[0] {
						http.Error(w, "conflicting timeline methods", http.StatusBadRequest)
						return
					}
				}
			}

			if method == "thread" || r.PostForm.Get("method") == "thread" {
				// A client that only POSTs still gets thread: the method
				// parameter is what names it, and the form is where the
				// parameters are.
				h.handleThread(w, r, r.Form)
				return
			}

			if method == "mark_read" || method == "mark_unread" || r.PostForm.Get("method") == "mark_read" || r.PostForm.Get("method") == "mark_unread" {
				method = r.Form.Get("method")
				values = r.Form
				channel := values.Get("channel")
				var markAsRead []string
				if uids, e := values["entry"]; e {
					markAsRead = uids
				} else if uids, e := values["entry[]"]; e {
					markAsRead = uids
				} else {
					uids := []string{}
					for k, v := range values {
						if entryRegex.MatchString(k) {
							uids = append(uids, v...)
						}
					}
					markAsRead = uids
				}

				if method == "mark_unread" {
					if channel == "" || len(markAsRead) == 0 {
						http.Error(w, "mark_unread requires a channel and at least one entry", http.StatusBadRequest)
						return
					}
					for _, uid := range markAsRead {
						if uid == "" {
							http.Error(w, "mark_unread requires nonempty entries", http.StatusBadRequest)
							return
						}
					}
				}
				if len(markAsRead) > 0 {
					var err error
					if method == "mark_unread" {
						err = h.backend.MarkUnread(r.Context(), channel, markAsRead)
					} else {
						err = h.backend.MarkRead(r.Context(), channel, markAsRead)
					}
					if err != nil {
						if errors.Is(err, microsub.ErrNotFound) {
							http.Error(w, "channel not found", http.StatusNotFound)
						} else {
							http.Error(w, err.Error(), http.StatusInternalServerError)
						}
						return
					}
				} else {
					// A client that drops its body (no Content-Type, say) used to get 200
					// here and report success; the entries stayed unread with no way to tell.
					slog.WarnContext(r.Context(), "mark called without any entries", "method", method, "channel", channel)
					http.Error(w, method+" requires at least one entry", http.StatusBadRequest)
					return
				}
			} else {
				http.Error(w, fmt.Sprintf("unknown method in timeline %s\n", method), http.StatusBadRequest)
				return
			}

			respondJSON(w, []string{})
		} else {
			http.Error(w, fmt.Sprintf("unknown action %s\n", action), http.StatusBadRequest)
		}
		return
	}
}

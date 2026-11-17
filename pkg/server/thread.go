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

package server

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// handleThread answers action=timeline&method=thread: the conversation around
// one entry, root first and the known replies oldest first.
//
// It is an extension to the published protocol, built on the existing timeline
// action so that a client which does not know it keeps working: it never sends
// method=thread, and an unknown method is a 400 rather than a silently wrong
// timeline.
func (h *microsubHandler) handleThread(w http.ResponseWriter, r *http.Request, values url.Values) {
	ctx := r.Context()

	channel := values.Get("channel")
	entry := values.Get("entry")
	targetURL := values.Get("url")

	// One anchor, not two. entry is an _id in one of the user's channels; url
	// is a permalink a client may have got from somewhere else.
	if (entry == "") == (targetURL == "") {
		http.Error(w, "thread needs exactly one of entry or url", http.StatusBadRequest)
		return
	}

	depth := microsub.DefaultThreadDepth
	if raw := values.Get("depth"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			http.Error(w, "depth must be a positive integer", http.StatusBadRequest)
			return
		}
		if n > microsub.MaxThreadDepth {
			n = microsub.MaxThreadDepth
		}
		depth = n
	}

	thread, err := h.backend.ThreadGet(ctx, channel, entry, targetURL, depth)
	if err != nil {
		if errors.Is(err, microsub.ErrNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "could not read the thread",
			"channel", channel, "entry", entry, "url", targetURL, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, thread)
}

/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 *  This program is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package observability

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Microsub is one endpoint with an `action` parameter, so the route label alone
// says nothing about what a request did: every timeline read, channel edit and
// follow arrives as "/microsub/". The action goes on the span and into the metric
// label, which is how a slow request becomes "timeline is slow" instead of
// "the microsub endpoint is slow".
const (
	microsubRoute = "/microsub/"

	attrAction  = "microsub.action"
	attrChannel = "microsub.channel"
)

// microsubActions is the closed set from the spec. The action is client-supplied,
// so an unknown value is reported as "other": otherwise a typo or a scanner
// probing `?action=` would create a metric series per string.
var microsubActions = map[string]struct{}{
	"channels":  {},
	"timeline":  {},
	"preview":   {},
	"search":    {},
	"follow":    {},
	"unfollow":  {},
	"mark-read": {},
	"mute":      {},
	"unmute":    {},
	"block":     {},
	"unblock":   {},
	"share":     {},
	"unshare":   {},
	"events":    {},
}

// actionOf reads the microsub action from the query or a form body. Values
// outside the spec collapse into "other".
func actionOf(r *http.Request) string {
	action := strings.TrimSpace(r.FormValue("action"))
	if action == "" {
		return ""
	}
	if _, ok := microsubActions[action]; ok {
		return action
	}
	return "other"
}

// channelOf names the channel a request is about. It stays out of metric labels:
// channels are user-created uids, and a label per uid is unbounded cardinality.
func channelOf(r *http.Request) string {
	return r.FormValue("channel")
}

// enrichRequest adds the microsub fields to the request's span. It runs inside
// the otelhttp handler so the span already exists, and does nothing when tracing
// is off or the route is not the microsub endpoint.
func enrichRequest(route string, r *http.Request) {
	if route != microsubRoute {
		return
	}

	span := trace.SpanFromContext(r.Context())
	if !span.IsRecording() {
		return
	}

	attrs := make([]attribute.KeyValue, 0, 2)
	if action := actionOf(r); action != "" {
		attrs = append(attrs, attribute.String(attrAction, action))
	}
	if channel := channelOf(r); channel != "" {
		attrs = append(attrs, attribute.String(attrChannel, channel))
	}
	span.SetAttributes(attrs...)
}

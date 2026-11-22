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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// The reader polls people's feeds on machines that are often a single small
// VPS or a Raspberry Pi. Two things follow from that: every request has to be
// cheap for them to serve, and it has to say who is asking so they can block
// us, contact us, or ask us to stop.

// defaultUserAgent identifies ekster. Sending no User-Agent means Go's
// "Go-http-client/1.1", which gives the operator of a small site nothing to act
// on when the traffic annoys them.
const defaultUserAgent = "ekster/1.0 (+https://github.com/pstuifzand/ekster)"

// acceptHeader lists the formats this reader understands, feeds first. A host
// that content-negotiates then answers with XML instead of rendering a whole
// HTML page, which is less work for it and a smaller body for us.
const acceptHeader = "application/atom+xml, application/rss+xml, application/feed+json;q=0.9, " +
	"application/json;q=0.8, application/xml;q=0.8, text/html;q=0.7"

// minFailureDelay and maxFailureDelay bound the retry schedule of a feed that
// keeps failing. Retrying a 404 every minute is the impoliteness this exists to
// stop: without a failure schedule the feed's next_fetch_at never advances, so
// a broken feed is fetched on every ticker tick.
const (
	minFailureDelay = 2 * time.Minute
	maxFailureDelay = 6 * time.Hour
	maxRetryAfter   = 24 * time.Hour
)

// transientFailureFirstDelay is the first rung for a failure that says nothing
// about the feed itself: a timeout, a reset connection, or a 5xx from a host
// that renders its answer when it is asked. The ordinary ladder starts at two
// minutes, which is longer than such a dependency stays broken — hnrss.org
// answers 502 for a few seconds and then serves the same query — so the first
// attempt after a transient failure comes back in half a minute. It is a
// schedule, not a retry: one request per attempt, as before, and a host that
// keeps failing climbs the same 2m/4m/8m ladder from there.
const transientFailureFirstDelay = 30 * time.Second

// failureKind says whether a failed fetch describes the moment or the feed. A
// transient failure is worth coming back for quickly; a terminal one will fail
// identically next time, so it belongs on the ordinary ladder.
type failureKind int

const (
	transientFailure failureKind = iota
	terminalFailure
)

// failureKindForStatus classifies what the host answered. A 5xx or a 429 says
// the host could not serve this request now; every other non-2xx says the URL
// is wrong, the feed is gone, or we are not allowed to read it.
func failureKindForStatus(status int) failureKind {
	if status >= 500 || status == http.StatusTooManyRequests {
		return transientFailure
	}
	return terminalFailure
}

// failureKindForError classifies a transport failure. A timeout, a reset or a
// refused connection is the network or the host having a moment; a name that
// does not resolve is a feed nobody will ever be able to fetch, so retrying it
// quickly only repeats the same DNS answer.
func failureKindForError(err error) failureKind {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return terminalFailure
	}
	return transientFailure
}

// failureSchedule picks the wait before the next attempt. failures counts the
// consecutive failures *before* this one, so only a transient failure on the
// first attempt gets the short rung; everything else takes the ladder.
func failureSchedule(kind failureKind, failures int) time.Duration {
	if kind == transientFailure && failures < 1 {
		return transientFailureFirstDelay
	}
	return failureDelay(failures + 1)
}

// userAgent reports the User-Agent to send. EKSTER_USER_AGENT overrides the
// default, so an operator can point at their own site or a contact address.
func userAgent() string {
	if ua := strings.TrimSpace(os.Getenv("EKSTER_USER_AGENT")); ua != "" {
		return ua
	}
	return defaultUserAgent
}

// feedRequest builds the GET for a feed URL, carrying the validators the server
// handed out the last time it answered with a body. Conditional requests are
// the difference between downloading a feed that has not changed and being
// told it has not changed: a 304 costs the host a header comparison and costs
// the reader no body.
func feedRequest(ctx context.Context, fetchURL, etag, lastModified, token string) (*http.Request, error) {
	if !strings.HasPrefix(fetchURL, "http") {
		return nil, fmt.Errorf("error parsing %s as url, has no http(s) prefix", fetchURL)
	}

	u, err := url.Parse(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
	}

	if strings.Contains(u.Host, "twitter.com") {
		if !strings.Contains(u.Path, "/status/") {
			return nil, ErrBlackList
		}
	}
	if strings.Contains(u.Host, "reddit.com") {
		return nil, ErrBlackList
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", acceptHeader)
	// A feed that is not public says so with a 401; the token is the answer. It
	// is added here, next to the conditional headers, so every fetch path gets it
	// from the same place.
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	return req, nil
}

// fetchFeedResponse performs a feed GET with the caller's context, so the span
// stays in the trace and the per-feed deadline cancels it.
func fetchFeedResponse(ctx context.Context, fetchURL, etag, lastModified, token string) (*http.Response, error) {
	req, err := feedRequest(ctx, fetchURL, etag, lastModified, token)
	if err != nil {
		return nil, err
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		// %w, not %s: the wrapped *url.Error is what tells a timeout from a
		// name that does not resolve, and the caller decides how soon to come
		// back from that.
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	return resp, nil
}

// failureDelay is how long to wait before retrying a feed after it failed
// `failures` times in a row: 2m, 4m, 8m and so on, capped at six hours.
func failureDelay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := minFailureDelay
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= maxFailureDelay {
			return maxFailureDelay
		}
	}
	return d
}

// retryAfterDelay reads a Retry-After header, which is what a host sends when
// it is being hammered or is deliberately closed for a while. Honouring it is
// cheaper than being blocked: the value is either a number of seconds or an
// HTTP date. Anything unparseable falls back to the failure schedule.
func retryAfterDelay(resp *http.Response, fallback time.Duration, now time.Time) time.Duration {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return fallback
	}

	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return fallback
		}
		return capDelay(time.Duration(secs) * time.Second)
	}

	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d <= 0 {
			return fallback
		}
		return capDelay(d)
	}

	return fallback
}

func capDelay(d time.Duration) time.Duration {
	if d < minFailureDelay {
		return minFailureDelay
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}

// identifyingTransport stamps the User-Agent on requests made through a client
// ekster builds itself: hub discovery (HEAD then GET) and the subscription
// POST. They used to go out as Go's anonymous default while feed polling
// identified itself, so a host saw two different visitors from one box and
// could act on neither.
type identifyingTransport struct{ next http.RoundTripper }

func (t identifyingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("User-Agent", userAgent())
		// A GET or HEAD here is discovery of a feed or hub: say which
		// representations we can use, so a host does not render HTML for us.
		if r.Header.Get("Accept") == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			r.Header.Set("Accept", acceptHeader)
		}
	}
	return t.next.RoundTrip(r)
}

// identifyingClient is an http.Client that introduces itself. http.DefaultTransport
// is read when the client is built, so a Tracing-enabled transport is picked up
// rather than bypassed.
// tokenTransport adds a bearer token to every request it carries. It exists so a
// private topic can be read by the same discovery code that reads public ones:
// the hub link is in the body, and the body is behind the token.
type tokenTransport struct {
	next  http.RoundTripper
	token string
}

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", userAgent())
	}
	return t.next.RoundTrip(r)
}

// tokenClient is the identifying client with a credential.
func tokenClient(token string) *http.Client {
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: tokenTransport{next: identifyingTransport{next: http.DefaultTransport}, token: token},
	}
}

func identifyingClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: identifyingTransport{next: http.DefaultTransport},
	}
}

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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The reader polls machines that are often one small VPS. The request has to
// say who is asking and what it understands, or the operator of that VPS has
// nothing to act on and gets a rendered HTML page where a feed would do.
func TestFeedRequestIdentifiesTheReaderAndAsksForFeeds(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte("<rss></rss>"))
	}))
	defer server.Close()

	resp, err := fetchFeedResponse(context.Background(), server.URL+"/feed", "", "")
	if err != nil {
		t.Fatalf("fetchFeedResponse: %s", err)
	}
	defer resp.Body.Close()

	if ua := got.Get("User-Agent"); !strings.HasPrefix(ua, "ekster/") {
		t.Errorf("User-Agent = %q, want an ekster identity", ua)
	}
	if accept := got.Get("Accept"); !strings.Contains(accept, "application/rss+xml") {
		t.Errorf("Accept = %q, want the feed formats first", accept)
	}
}

// An unchanged feed must be asked for conditionally. Downloading it again is
// the request this reader should not be making.
func TestFeedRequestCarriesTheValidators(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.Header.Get("If-None-Match") != `W/"abc"` {
			t.Errorf("If-None-Match = %q", r.Header.Get("If-None-Match"))
		}
		if !strings.HasPrefix(r.Header.Get("If-Modified-Since"), "Mon, 02 Jan 2006") {
			t.Errorf("If-Modified-Since = %q", r.Header.Get("If-Modified-Since"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	resp, err := fetchFeedResponse(context.Background(), server.URL+"/feed", `W/"abc"`, "Mon, 02 Jan 2006 15:04:05 GMT")
	if err != nil {
		t.Fatalf("fetchFeedResponse: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("StatusCode = %d, want 304", resp.StatusCode)
	}
	if got.Get("If-None-Match") == "" || got.Get("If-Modified-Since") == "" {
		t.Errorf("the conditional headers were not sent: %v", got)
	}
}

// An operator has to be able to point the reader at their own site.
func TestUserAgentCanBeOverridden(t *testing.T) {
	t.Setenv("EKSTER_USER_AGENT", "p83 reader (https://p83.nl/)")

	if got := userAgent(); got != "p83 reader (https://p83.nl/)" {
		t.Errorf("userAgent() = %q", got)
	}
}

// A feed that keeps failing must be retried less and less often. Retrying on
// every ticker tick is a request a minute against a host that already said no.
func TestFailureDelayBacksOffAndCaps(t *testing.T) {
	if got := failureDelay(1); got != 2*time.Minute {
		t.Errorf("failureDelay(1) = %s, want 2m", got)
	}
	if got := failureDelay(2); got != 4*time.Minute {
		t.Errorf("failureDelay(2) = %s, want 4m", got)
	}
	if got := failureDelay(3); got != 8*time.Minute {
		t.Errorf("failureDelay(3) = %s, want 8m", got)
	}

	previous := time.Duration(0)
	for i := 1; i <= 20; i++ {
		d := failureDelay(i)
		if d < previous {
			t.Fatalf("failureDelay(%d) = %s went backwards from %s", i, d, previous)
		}
		if d > maxFailureDelay {
			t.Fatalf("failureDelay(%d) = %s exceeds the %s cap", i, d, maxFailureDelay)
		}
		previous = d
	}
	if got := failureDelay(50); got != maxFailureDelay {
		t.Errorf("failureDelay(50) = %s, want the cap %s", got, maxFailureDelay)
	}
}

// Retry-After is the host telling us when to come back. Ignoring it is how a
// rate limit turns into a block.
func TestRetryAfterIsHonoured(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600")
	if got := retryAfterDelay(resp, time.Minute, now); got != time.Hour {
		t.Errorf("seconds form: got %s, want 1h", got)
	}

	resp.Header.Set("Retry-After", now.Add(90*time.Minute).Format(http.TimeFormat))
	if got := retryAfterDelay(resp, time.Minute, now); got != 90*time.Minute {
		t.Errorf("date form: got %s, want 90m", got)
	}

	resp.Header.Set("Retry-After", "not a delay")
	if got := retryAfterDelay(resp, 7*time.Minute, now); got != 7*time.Minute {
		t.Errorf("garbage: got %s, want the fallback", got)
	}

	resp.Header.Set("Retry-After", "31536000")
	if got := retryAfterDelay(resp, time.Minute, now); got > maxRetryAfter {
		t.Errorf("a year-long Retry-After was not capped: %s", got)
	}

	resp.Header.Del("Retry-After")
	if got := retryAfterDelay(resp, 7*time.Minute, now); got != 7*time.Minute {
		t.Errorf("missing header: got %s, want the fallback", got)
	}
}

// The blacklist still applies to the shared builder, now that Fetch2 and the
// feed path both go through it.
func TestFeedRequestRejectsBlacklistedHosts(t *testing.T) {
	if _, err := feedRequest(context.Background(), "https://twitter.com/pstuifzand", "", ""); err != ErrBlackList {
		t.Errorf("twitter profile: err = %v, want ErrBlackList", err)
	}
	if _, err := feedRequest(context.Background(), "https://www.reddit.com/r/golang/.rss", "", ""); err != ErrBlackList {
		t.Errorf("reddit: err = %v, want ErrBlackList", err)
	}
	if _, err := feedRequest(context.Background(), "https://twitter.com/pstuifzand/status/1", "", ""); err != nil {
		t.Errorf("a tweet link: err = %v, want nil", err)
	}
}

// Hub discovery and the subscription POST go through a client ekster builds
// itself; those requests must not arrive as Go's anonymous default.
func TestIdentifyingClientSetsTheUserAgent(t *testing.T) {
	var ua, method, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		method = r.Method
		accept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := identifyingClient(5 * time.Second).Head(server.URL + "/feed")
	if err != nil {
		t.Fatalf("HEAD: %s", err)
	}
	resp.Body.Close()

	if !strings.HasPrefix(ua, "ekster/") {
		t.Errorf("%s User-Agent = %q, want an ekster identity", method, ua)
	}
	if !strings.Contains(accept, "application/rss+xml") {
		t.Errorf("%s Accept = %q, want the feed formats", method, accept)
	}
}

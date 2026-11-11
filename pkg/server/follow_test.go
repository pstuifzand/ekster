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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// followBackend answers FollowURL with a canned error, so the handler's status
// mapping can be tested without a database.
type followBackend struct {
	*NullBackend
	err error
}

func (b *followBackend) FollowURL(ctx context.Context, uid, feedURL string) (microsub.Feed, error) {
	if b.err != nil {
		return microsub.Feed{Type: "feed", URL: feedURL}, b.err
	}
	return microsub.Feed{Type: "feed", URL: feedURL}, nil
}

func postFollow(t *testing.T, backend microsub.Microsub) *http.Response {
	t.Helper()
	handler, _ := NewMicrosubHandler(backend)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	body := url.Values{
		"action":  {"follow"},
		"channel": {"0001"},
		"url":     {"https://ramblinggit.com/"},
	}.Encode()

	resp, err := http.Post(server.URL, "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST follow: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// Following a URL that is already followed in another channel is a client
// error, not a server failure: the unique constraint on feeds.url used to leak
// out as a 500 carrying the raw pq error.
func TestServer_FollowURL_AlreadyFollowedIsConflict(t *testing.T) {
	backend := &followBackend{
		NullBackend: &NullBackend{},
		err:         fmt.Errorf("%w: %s", microsub.ErrFeedAlreadyFollowed, "https://ramblinggit.com/"),
	}

	resp := postFollow(t, backend)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

// An unrelated backend failure stays a 500.
func TestServer_FollowURL_OtherErrorIsInternalServerError(t *testing.T) {
	backend := &followBackend{
		NullBackend: &NullBackend{},
		err:         errors.New("fetch failed"),
	}

	resp := postFollow(t, backend)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// Re-following a URL already in this channel is idempotent, so it answers 200.
func TestServer_FollowURL_RepeatIsOK(t *testing.T) {
	resp := postFollow(t, &followBackend{NullBackend: &NullBackend{}})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

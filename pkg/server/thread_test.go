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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threadBackend records what the handler asked for and answers with a canned
// thread. It embeds NullBackend so the other actions keep their null behaviour
// instead of stuttering out a dozen empty methods.
type threadBackend struct {
	NullBackend

	called  bool
	channel string
	entry   string
	url     string
	depth   int

	thread microsub.Thread
	err    error
}

func (b *threadBackend) ThreadGet(ctx context.Context, channel, entry, url string, depth int) (microsub.Thread, error) {
	b.called = true
	b.channel, b.entry, b.url, b.depth = channel, entry, url, depth
	if b.err != nil {
		return microsub.Thread{}, b.err
	}
	if b.thread.Items == nil {
		b.thread.Items = []microsub.Item{}
	}
	return b.thread, nil
}

func threadRequest(t *testing.T, backend *threadBackend, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	handler, _ := NewMicrosubHandler(backend)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestThreadGetByEntry(t *testing.T) {
	backend := &threadBackend{thread: microsub.Thread{
		Items:  []microsub.Item{{Type: "entry", ID: "b3d1", URL: "https://bob.example/re"}},
		Thread: microsub.ThreadInfo{Root: "https://ann.example/root"},
	}}

	req := httptest.NewRequest("GET", "/microsub?action=timeline&method=thread&channel=home&entry=b3d1", nil)
	recorder := threadRequest(t, backend, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, backend.called)
	assert.Equal(t, "home", backend.channel)
	assert.Equal(t, "b3d1", backend.entry)
	assert.Equal(t, "", backend.url)
	assert.Equal(t, microsub.DefaultThreadDepth, backend.depth, "depth defaults when the client omits it")

	var got microsub.Thread
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
	assert.Equal(t, "https://ann.example/root", got.Thread.Root)
	require.Len(t, got.Items, 1)
	assert.Equal(t, "b3d1", got.Items[0].ID)
}

func TestThreadGetByURL(t *testing.T) {
	backend := &threadBackend{}
	req := httptest.NewRequest("GET", "/microsub?action=timeline&method=thread&url=https%3A%2F%2Fbob.example%2Fre", nil)
	recorder := threadRequest(t, backend, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "https://bob.example/re", backend.url)
	assert.Equal(t, "", backend.entry)
}

// A POST is what a client that only speaks forms will send, and the method
// parameter is the same either way. (A form body without the form content type
// is the bug this repo already paid for once: ParseForm reads nothing.)
func TestThreadGetAcceptsPOST(t *testing.T) {
	backend := &threadBackend{}

	form := url.Values{
		"action": {"timeline"},
		"method": {"thread"},
		"entry":  {"b3d1"},
	}
	req := httptest.NewRequest("POST", "/microsub", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := threadRequest(t, backend, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, backend.called)
	assert.Equal(t, "b3d1", backend.entry)
}

func TestThreadRequiresExactlyOneAnchor(t *testing.T) {
	for name, query := range map[string]string{
		"neither": "",
		"both":    "&entry=b3d1&url=https%3A%2F%2Fbob.example%2Fre",
	} {
		backend := &threadBackend{}
		req := httptest.NewRequest("GET", "/microsub?action=timeline&method=thread"+query, nil)
		recorder := threadRequest(t, backend, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code, name)
		assert.False(t, backend.called, name)
	}
}

func TestThreadMissingEntryIsNotFound(t *testing.T) {
	backend := &threadBackend{err: microsub.ErrNotFound}
	req := httptest.NewRequest("GET", "/microsub?action=timeline&method=thread&entry=nope", nil)
	recorder := threadRequest(t, backend, req)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "nope", "the answer must not echo what it could not find")
}

func TestThreadDepthIsValidatedAndCapped(t *testing.T) {
	backend := &threadBackend{}
	req := httptest.NewRequest("GET", "/microsub?action=timeline&method=thread&entry=b3d1&depth=abc", nil)
	assert.Equal(t, http.StatusBadRequest, threadRequest(t, backend, req).Code, "a depth that is not a number")
	assert.False(t, backend.called)

	backend = &threadBackend{}
	req = httptest.NewRequest("GET", "/microsub?action=timeline&method=thread&entry=b3d1&depth=99", nil)
	require.Equal(t, http.StatusOK, threadRequest(t, backend, req).Code)
	assert.Equal(t, microsub.MaxThreadDepth, backend.depth, "depth is capped, not refused")

	backend = &threadBackend{}
	req = httptest.NewRequest("GET", "/microsub?action=timeline&method=thread&entry=b3d1&depth=3", nil)
	require.Equal(t, http.StatusOK, threadRequest(t, backend, req).Code)
	assert.Equal(t, 3, backend.depth)
}

// A client that predates the extension never sends method=thread; the plain
// timeline must keep answering 200, and an unknown method must be a client
// error rather than a 500.
func TestTimelineStillWorksAndUnknownMethodsAre400(t *testing.T) {
	backend := &threadBackend{}

	req := httptest.NewRequest("GET", "/microsub?action=timeline&channel=0000", nil)
	assert.Equal(t, http.StatusOK, threadRequest(t, backend, req).Code)
	assert.False(t, backend.called)

	form := url.Values{"action": {"timeline"}, "method": {"nonsense"}}
	req = httptest.NewRequest("POST", "/microsub", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	assert.Equal(t, http.StatusBadRequest, threadRequest(t, backend, req).Code)
}

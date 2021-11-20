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
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"p83.nl/go/ekster/pkg/client"
	"p83.nl/go/ekster/pkg/microsub"
)

func init() {
	log.SetOutput(ioutil.Discard)
}

func createServerClient() (*httptest.Server, *client.Client) {
	backend := &NullBackend{}

	handler, _ := NewMicrosubHandler(backend)

	server := httptest.NewServer(handler)

	c := client.Client{
		Logging: false,
		Token:   "1234",
	}

	c.Me, _ = url.Parse("https://example.com/")
	c.MicrosubEndpoint, _ = url.Parse(server.URL + "/microsub")

	return server, &c
}

func TestServer_ChannelsGetList(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()

	channels, err := c.ChannelsGetList()

	if assert.NoError(t, err) {
		assert.Equal(t, 2, len(channels), "should return 2 channels")

		assert.Equal(t, "notifications", channels[0].Name)
		assert.Equal(t, "0001", channels[0].UID)
		assert.Equal(t, microsub.Unread{Type: microsub.UnreadBool}, channels[0].Unread)

		assert.Equal(t, "default", channels[1].Name)
		assert.Equal(t, "0000", channels[1].UID)
		assert.Equal(t, microsub.Unread{Type: microsub.UnreadBool}, channels[0].Unread)
	}
}

func TestServer_ChannelsCreate(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()

	channel, err := c.ChannelsCreate("test")

	if assert.NoError(t, err) {
		assert.Equal(t, "test", channel.Name)
	}
}

func TestServer_ChannelsDelete(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	err := c.ChannelsDelete("0001")
	assert.NoError(t, err)
}

func TestServer_ChannelsUpdate(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	_, err := c.ChannelsUpdate("0001", "newname")
	assert.NoError(t, err)
}

func TestServer_TimelineGet(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	timeline, err := c.TimelineGet("", "", "0001")
	if assert.NoError(t, err) {
		assert.Equal(t, 0, len(timeline.Items))
	}
}

func TestServer_FollowGetList(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	feeds, err := c.FollowGetList("0001")
	if assert.NoError(t, err) {
		assert.Equal(t, 1, len(feeds))
		assert.Equal(t, "test", feeds[0].Name)
		assert.Equal(t, "feed", feeds[0].Type)
		assert.Equal(t, "https://example.com/", feeds[0].URL)
	}
}

func TestServer_FollowURL(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	feed, err := c.FollowURL("0001", "https://example.com/")
	if assert.NoError(t, err) {
		assert.Equal(t, "feed", feed.Type)
		assert.Equal(t, "https://example.com/", feed.URL)
	}
}

func TestServer_UnFollowURL(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	err := c.UnfollowURL("0001", "https://example.com/")
	assert.NoError(t, err)
}

func TestServer_PreviewURL(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	timeline, err := c.PreviewURL("https://example.com/")
	if assert.NoError(t, err) {
		assert.Equal(t, 0, len(timeline.Items))
	}
}

func TestServer_Search(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	feeds, err := c.Search("https://example.com/")
	if assert.NoError(t, err) {
		assert.Equal(t, 1, len(feeds))
		assert.Equal(t, "feed", feeds[0].Type)
		assert.Equal(t, "https://example.com/", feeds[0].URL)
		assert.Equal(t, "Example", feeds[0].Name)
		assert.Equal(t, "test.jpg", feeds[0].Photo)
		assert.Equal(t, "test", feeds[0].Description)
	}
}

func TestServer_MarkRead(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()
	err := c.MarkRead("0001", []string{"test"})
	assert.NoError(t, err)
}

func TestServer_GetUnknownAction(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()

	u := c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", "missing")
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if assert.NoError(t, err) {
		assert.Equal(t, 400, resp.StatusCode)
	}
}
func TestServer_PostUnknownAction(t *testing.T) {
	server, c := createServerClient()
	defer server.Close()

	u := c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", "missing")
	u.RawQuery = q.Encode()

	resp, err := http.Post(u.String(), "application/json", nil)
	if assert.NoError(t, err) {
		assert.Equal(t, 400, resp.StatusCode)
	}
}

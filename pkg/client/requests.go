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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/sse"
)

// Client is a HTTP client for Microsub
type Client struct {
	Me               *url.URL
	MicrosubEndpoint *url.URL
	Token            string

	Logging bool
}

func (c *Client) microsubGetRequest(ctx context.Context, action string, args map[string]string) (*http.Response, error) {
	client := http.Client{}

	u := *c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", action)
	for k, v := range args {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	if c.Logging {
		x, _ := httputil.DumpRequestOut(req, true)
		log.Printf("REQUEST:\n\n%s\n\n", x)
	}

	res, err := client.Do(req)

	if c.Logging {
		x, _ := httputil.DumpResponse(res, true)
		log.Printf("RESPONSE:\n\n%s\n\n", x)
	}

	return res, err
}

func (c *Client) microsubPostRequest(ctx context.Context, action string, args map[string]string) (*http.Response, error) {
	client := http.Client{}

	u := *c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", action)
	for k, v := range args {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	if c.Logging {
		x, _ := httputil.DumpRequestOut(req, true)
		log.Printf("REQUEST:\n\n%s\n\n", x)
	}

	res, err := client.Do(req)

	if c.Logging {
		x, _ := httputil.DumpResponse(res, true)
		log.Printf("RESPONSE:\n\n%s\n\n", x)
	}

	if res.StatusCode != 200 {
		msg, _ := ioutil.ReadAll(res.Body)
		return nil, fmt.Errorf("unsuccessful response: %d: %q", res.StatusCode, strings.TrimSpace(string(msg)))
	}

	return res, err
}

func (c *Client) microsubPostFormRequest(ctx context.Context, action string, args map[string]string, data url.Values) (*http.Response, error) {
	client := http.Client{}

	u := *c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", action)
	for k, v := range args {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	res, err := client.Do(req)

	if res.StatusCode != 200 {
		msg, _ := ioutil.ReadAll(res.Body)
		return nil, fmt.Errorf("unsuccessful response: %d: %q", res.StatusCode, strings.TrimSpace(string(msg)))
	}

	return res, err
}

// ChannelsGetList gets the channels from a Microsub server
func (c *Client) ChannelsGetList(ctx context.Context) ([]microsub.Channel, error) {
	args := make(map[string]string)
	res, err := c.microsubGetRequest(ctx, "channels", args)
	if err != nil {
		return []microsub.Channel{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return []microsub.Channel{}, fmt.Errorf("HTTP Status is not 200, but %d, error while reading body", res.StatusCode)
		}
		return []microsub.Channel{}, fmt.Errorf("HTTP Status is not 200, but %d: %s", res.StatusCode, body)
	}

	type channelsResponse struct {
		Channels []microsub.Channel `json:"channels"`
	}

	dec := json.NewDecoder(res.Body)
	var channels channelsResponse
	err = dec.Decode(&channels)

	return channels.Channels, err
}

// TimelineGet gets a timeline from a Microsub server
func (c *Client) TimelineGet(ctx context.Context, before, after, channel string) (microsub.Timeline, error) {
	args := make(map[string]string)
	args["after"] = after
	args["before"] = before
	args["channel"] = channel
	res, err := c.microsubGetRequest(ctx, "timeline", args)
	if err != nil {
		return microsub.Timeline{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return microsub.Timeline{}, fmt.Errorf("HTTP Status is not 200, but %d, error while reading body", res.StatusCode)
		}
		return microsub.Timeline{}, fmt.Errorf("HTTP Status is not 200, but %d: %s", res.StatusCode, body)
	}
	dec := json.NewDecoder(res.Body)
	var timeline microsub.Timeline
	err = dec.Decode(&timeline)
	if err != nil {
		return microsub.Timeline{}, err
	}
	return timeline, nil
}

// PreviewURL gets a Timeline for a url from a Microsub server
func (c *Client) PreviewURL(ctx context.Context, url string) (microsub.Timeline, error) {
	args := make(map[string]string)
	args["url"] = url
	res, err := c.microsubPostRequest(ctx, "preview", args)
	if err != nil {
		return microsub.Timeline{}, err
	}
	defer res.Body.Close()

	var timeline microsub.Timeline
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return timeline, fmt.Errorf("HTTP Status is not 200, but %d, error while reading body", res.StatusCode)
		}
		return timeline, fmt.Errorf("HTTP Status is not 200, but %d: %s", res.StatusCode, body)
	}
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&timeline)
	if err != nil {
		return microsub.Timeline{}, err
	}
	return timeline, nil
}

// FollowGetList gets the list of followed feeds.
func (c *Client) FollowGetList(ctx context.Context, channel string) ([]microsub.Feed, error) {
	args := make(map[string]string)
	args["channel"] = channel
	res, err := c.microsubGetRequest(ctx, "follow", args)
	if err != nil {
		return []microsub.Feed{}, nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return []microsub.Feed{}, fmt.Errorf("HTTP Status is not 200, but %d, error while reading body", res.StatusCode)
		}
		return []microsub.Feed{}, fmt.Errorf("HTTP Status is not 200, but %d: %s", res.StatusCode, body)
	}
	dec := json.NewDecoder(res.Body)
	type followResponse struct {
		Items []microsub.Feed `json:"items"`
	}
	var response followResponse
	err = dec.Decode(&response)
	if err != nil {
		return []microsub.Feed{}, nil
	}
	return response.Items, nil
}

// ChannelsCreate creates and new channel on a microsub server.
func (c *Client) ChannelsCreate(ctx context.Context, name string) (microsub.Channel, error) {
	args := make(map[string]string)
	args["name"] = name
	res, err := c.microsubPostRequest(ctx, "channels", args)
	if err != nil {
		return microsub.Channel{}, nil
	}
	defer res.Body.Close()
	var channel microsub.Channel
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&channel)
	if err != nil {
		return microsub.Channel{}, nil
	}
	return channel, nil
}

// ChannelsUpdate updates a channel.
func (c *Client) ChannelsUpdate(ctx context.Context, uid, name string) (microsub.Channel, error) {
	args := make(map[string]string)
	args["name"] = name
	args["channel"] = uid
	res, err := c.microsubPostRequest(ctx, "channels", args)
	if err != nil {
		return microsub.Channel{}, err
	}
	defer res.Body.Close()
	var channel microsub.Channel
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&channel)
	if err != nil {
		return microsub.Channel{}, err
	}
	return channel, nil
}

// ChannelsDelete deletes a channel.
func (c *Client) ChannelsDelete(ctx context.Context, uid string) error {
	args := make(map[string]string)
	args["channel"] = uid
	args["method"] = "delete"
	res, err := c.microsubPostRequest(ctx, "channels", args)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// FollowURL follows a url.
func (c *Client) FollowURL(ctx context.Context, channel, url string) (microsub.Feed, error) {
	args := make(map[string]string)
	args["channel"] = channel
	args["url"] = url
	res, err := c.microsubPostRequest(ctx, "follow", args)
	if err != nil {
		return microsub.Feed{}, err
	}
	defer res.Body.Close()
	var feed microsub.Feed
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&feed)
	if err != nil {
		return microsub.Feed{}, err
	}
	return feed, nil
}

// UnfollowURL unfollows a url in a channel.
func (c *Client) UnfollowURL(ctx context.Context, channel, url string) error {
	args := make(map[string]string)
	args["channel"] = channel
	args["url"] = url
	res, err := c.microsubPostRequest(ctx, "unfollow", args)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// Search asks the server to search for the query.
func (c *Client) Search(ctx context.Context, query string) ([]microsub.Feed, error) {
	args := make(map[string]string)
	args["query"] = query
	res, err := c.microsubPostRequest(ctx, "search", args)
	if err != nil {
		return []microsub.Feed{}, err
	}
	type searchResponse struct {
		Results []microsub.Feed `json:"results"`
	}
	defer res.Body.Close()
	var response searchResponse
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&response)
	if err != nil {
		return []microsub.Feed{}, err
	}
	return response.Results, nil
}

// ItemSearch send a search request to the server
func (c *Client) ItemSearch(ctx context.Context, channel, query string) ([]microsub.Item, error) {
	args := make(map[string]string)
	args["query"] = query
	args["channel"] = channel
	res, err := c.microsubPostRequest(ctx, "search", args)
	if err != nil {
		return []microsub.Item{}, err
	}
	type searchResponse struct {
		Items []microsub.Item `json:"items"`
	}
	defer res.Body.Close()
	var response searchResponse
	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&response)
	if err != nil {
		return []microsub.Item{}, err
	}
	return response.Items, nil
}

// MarkRead marks an item read on the server.
func (c *Client) MarkRead(ctx context.Context, channel string, uids []string) error {
	args := make(map[string]string)
	args["channel"] = channel
	args["method"] = "mark_read"

	data := url.Values{}
	for _, uid := range uids {
		data.Add("entry[]", uid)
	}

	res, err := c.microsubPostFormRequest(ctx, "timeline", args, data)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// Events open an event channel to the server.
func (c *Client) Events(ctx context.Context) (chan sse.Message, error) {

	ch := make(chan sse.Message)

	errorCounter := 0
	go func() {
		for {
			res, err := c.microsubGetRequest(ctx, "events", nil)
			if err != nil {
				log.Printf("could not request events: %+v", err)
				errorCounter++
				if errorCounter > 5 {
					break
				}
				continue
			}

			err = sse.Reader(res.Body, ch)
			if err != nil {
				log.Printf("could not create reader: %+v", err)
				break
			}
		}

		close(ch)
	}()

	return ch, nil
}

package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"p83.nl/go/ekster/pkg/microsub"
)

type Client struct {
	Me               *url.URL
	MicrosubEndpoint *url.URL
	Token            string

	Logging bool
}

func (c *Client) microsubGetRequest(action string, args map[string]string) (*http.Response, error) {
	client := http.Client{}

	u := *c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", action)
	for k, v := range args {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
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

func (c *Client) microsubPostRequest(action string, args map[string]string) (*http.Response, error) {
	client := http.Client{}

	u := *c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", action)
	for k, v := range args {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
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
		return nil, fmt.Errorf("unsuccessful response: %d: %q", res.StatusCode, string(msg))
	}

	return res, err
}

func (c *Client) microsubPostFormRequest(action string, args map[string]string, data url.Values) (*http.Response, error) {
	client := http.Client{}

	u := *c.MicrosubEndpoint
	q := u.Query()
	q.Add("action", action)
	for k, v := range args {
		q.Add(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))

	res, err := client.Do(req)

	if res.StatusCode != 200 {
		msg, _ := ioutil.ReadAll(res.Body)
		return nil, fmt.Errorf("unsuccessful response: %d: %q", res.StatusCode, string(msg))
	}

	return res, err
}

func (c *Client) ChannelsGetList() ([]microsub.Channel, error) {
	args := make(map[string]string)
	res, err := c.microsubGetRequest("channels", args)
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

func (c *Client) TimelineGet(before, after, channel string) (microsub.Timeline, error) {
	args := make(map[string]string)
	args["after"] = after
	args["before"] = before
	args["channel"] = channel
	res, err := c.microsubGetRequest("timeline", args)
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

func (c *Client) PreviewURL(url string) (microsub.Timeline, error) {
	args := make(map[string]string)
	args["url"] = url
	res, err := c.microsubGetRequest("preview", args)
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

func (c *Client) FollowGetList(channel string) ([]microsub.Feed, error) {
	args := make(map[string]string)
	args["channel"] = channel
	res, err := c.microsubGetRequest("follow", args)
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

func (c *Client) ChannelsCreate(name string) (microsub.Channel, error) {
	args := make(map[string]string)
	args["name"] = name
	res, err := c.microsubPostRequest("channels", args)
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

func (c *Client) ChannelsUpdate(uid, name string) (microsub.Channel, error) {
	args := make(map[string]string)
	args["name"] = name
	args["uid"] = uid
	res, err := c.microsubPostRequest("channels", args)
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

func (c *Client) ChannelsDelete(uid string) error {
	args := make(map[string]string)
	args["channel"] = uid
	args["method"] = "delete"
	res, err := c.microsubPostRequest("channels", args)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Client) FollowURL(channel, url string) (microsub.Feed, error) {
	args := make(map[string]string)
	args["channel"] = channel
	args["url"] = url
	res, err := c.microsubPostRequest("follow", args)
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

func (c *Client) UnfollowURL(channel, url string) error {
	args := make(map[string]string)
	args["channel"] = channel
	args["url"] = url
	res, err := c.microsubPostRequest("unfollow", args)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Client) Search(query string) ([]microsub.Feed, error) {
	args := make(map[string]string)
	args["query"] = query
	res, err := c.microsubPostRequest("search", args)
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

func (c *Client) MarkRead(channel string, uids []string) error {
	args := make(map[string]string)
	args["channel"] = channel

	data := url.Values{}
	for _, uid := range uids {
		data.Add("entry[]", uid)
	}

	res, err := c.microsubPostFormRequest("mark_read", args, data)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Client) AddEventListener(el microsub.EventListener) error {
	panic("implement me")
}

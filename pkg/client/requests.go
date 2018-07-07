package client

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

type Client struct {
	Me               *url.URL
	MicrosubEndpoint *url.URL
	Token            string
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

	return client.Do(req)
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

	return client.Do(req)
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

	return client.Do(req)
}

func (c *Client) ChannelsGetList() []microsub.Channel {
	args := make(map[string]string)
	res, err := c.microsubGetRequest("channels", args)
	if err != nil {
		log.Println(err)
		return []microsub.Channel{}
	}
	defer res.Body.Close()

	type channelsResponse struct {
		Channels []microsub.Channel `json:"channels"`
	}

	dec := json.NewDecoder(res.Body)
	var channels channelsResponse
	dec.Decode(&channels)
	return channels.Channels
}

func (c *Client) TimelineGet(before, after, channel string) microsub.Timeline {
	args := make(map[string]string)
	args["after"] = after
	args["before"] = before
	args["channel"] = channel
	res, err := c.microsubGetRequest("timeline", args)
	if err != nil {
		log.Println(err)
		return microsub.Timeline{}
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	var timeline microsub.Timeline
	err = dec.Decode(&timeline)
	if err != nil {
		log.Fatal(err)
	}
	return timeline
}

func (c *Client) PreviewURL(url string) microsub.Timeline {
	args := make(map[string]string)
	args["url"] = url
	res, err := c.microsubGetRequest("preview", args)
	if err != nil {
		log.Println(err)
		return microsub.Timeline{}
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	var timeline microsub.Timeline
	dec.Decode(&timeline)
	return timeline
}

func (c *Client) FollowGetList(channel string) []microsub.Feed {
	args := make(map[string]string)
	args["channel"] = channel
	res, err := c.microsubGetRequest("follow", args)
	if err != nil {
		log.Println(err)
		return []microsub.Feed{}
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	type followResponse struct {
		Items []microsub.Feed `json:"items"`
	}
	var response followResponse
	dec.Decode(&response)
	return response.Items
}

func (c *Client) ChannelsCreate(name string) microsub.Channel {
	args := make(map[string]string)
	args["name"] = name
	res, err := c.microsubPostRequest("channels", args)
	if err != nil {
		log.Println(err)
		return microsub.Channel{}
	}
	defer res.Body.Close()
	var channel microsub.Channel
	dec := json.NewDecoder(res.Body)
	dec.Decode(&channel)
	return channel
}

func (c *Client) ChannelsUpdate(uid, name string) microsub.Channel {
	args := make(map[string]string)
	args["name"] = name
	args["uid"] = uid
	res, err := c.microsubPostRequest("channels", args)
	if err != nil {
		log.Println(err)
		return microsub.Channel{}
	}
	defer res.Body.Close()
	var channel microsub.Channel
	dec := json.NewDecoder(res.Body)
	dec.Decode(&channel)
	return channel
}

func (c *Client) ChannelsDelete(uid string) {
	args := make(map[string]string)
	args["channel"] = uid
	args["method"] = "delete"
	res, err := c.microsubPostRequest("channels", args)
	if err != nil {
		log.Println(err)
		return
	}
	res.Body.Close()
}

func (c *Client) FollowURL(channel, url string) microsub.Feed {
	args := make(map[string]string)
	args["channel"] = channel
	args["url"] = url
	res, err := c.microsubPostRequest("follow", args)
	if err != nil {
		log.Println(err)
		return microsub.Feed{}
	}
	defer res.Body.Close()
	var feed microsub.Feed
	dec := json.NewDecoder(res.Body)
	dec.Decode(&feed)
	return feed
}

func (c *Client) UnfollowURL(channel, url string) {
	args := make(map[string]string)
	args["channel"] = channel
	args["url"] = url
	res, err := c.microsubPostRequest("unfollow", args)
	if err != nil {
		log.Println(err)
		return
	}
	res.Body.Close()
}

func (c *Client) Search(query string) []microsub.Feed {
	args := make(map[string]string)
	args["query"] = query
	res, err := c.microsubPostRequest("search", args)
	if err != nil {
		log.Println(err)
		return []microsub.Feed{}
	}
	type searchResponse struct {
		Results []microsub.Feed `json:"results"`
	}
	defer res.Body.Close()
	var response searchResponse
	dec := json.NewDecoder(res.Body)
	dec.Decode(&response)
	return response.Results
}

func (c *Client) MarkRead(channel string, uids []string) {
	args := make(map[string]string)
	args["channel"] = channel

	data := url.Values{}
	for _, uid := range uids {
		data.Add("entry[]", uid)
	}

	res, err := c.microsubPostFormRequest("mark_read", args, data)
	if err == nil {
		defer res.Body.Close()
	}
}

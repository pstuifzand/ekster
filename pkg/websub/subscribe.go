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

package websub

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/pstuifzand/ekster/pkg/rss"

	"github.com/pstuifzand/ekster/pkg/linkheader"

	"github.com/pstuifzand/ekster/pkg/jsonfeed"
	"willnorris.com/go/microformats"
)

// GetHubURL finds the HubURL for topic
func GetHubURL(client *http.Client, topic string) (string, error) {
	hubURL, err := parseLinkHeaders(client, topic)
	if err == nil {
		return hubURL, err
	}

	hubURL, err = parseBodyLinks(client, topic)
	if err == nil {
		return hubURL, err
	}

	return "", fmt.Errorf("no hub url found for topic %s", topic)
}

func isFeedContentType(contentType string) bool {
	if strings.HasPrefix(contentType, "application/rss+xml") {
		return true
	}
	if strings.HasPrefix(contentType, "application/atom+xml") {
		return true
	}
	if strings.HasPrefix(contentType, "application/xml") {
		return true
	}
	if strings.HasPrefix(contentType, "text/xml") {
		return true
	}

	return false
}

func parseBodyLinks(client *http.Client, topic string) (string, error) {
	resp, err := client.Get(topic)

	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if isFeedContentType(contentType) {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		feed, err := rss.Parse(body)
		if err != nil {
			return "", err
		}

		if feed.HubURL != "" {
			return feed.HubURL, nil
		}
		return "", fmt.Errorf("no WebSub hub url found in the RSS feed")
	} else if strings.HasPrefix(contentType, "text/html") {
		topicURL, _ := url.Parse(topic)
		md := microformats.Parse(resp.Body, topicURL)
		if hubs, e := md.Rels["hub"]; e {
			if len(hubs) >= 1 {
				return hubs[0], nil
			}
		}
		return "", fmt.Errorf("no WebSub hub url found in HTML <link> elements")
	} else if strings.HasPrefix(contentType, "application/json") {
		var feed jsonfeed.Feed
		dec := json.NewDecoder(resp.Body)
		err := dec.Decode(&feed)
		if err != nil {
			log.Printf("error while parsing json feed: %s\n", err)
			return "", err
		}

		for _, v := range feed.Hubs {
			if v.Type == "WebSub" {
				return v.URL, nil
			}
		}

		return "", fmt.Errorf("no WebSub hub url found in jsonfeed")
	}

	return "", fmt.Errorf("unknown content type of response: %s", resp.Header.Get("Content-Type"))
}

func parseLinkHeaders(client *http.Client, topic string) (string, error) {
	resp, err := client.Head(topic)

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if headers, e := resp.Header["Link"]; e {
		links := linkheader.ParseMultiple(headers)
		for _, link := range links {
			if link.Rel == "hub" {
				return link.URL, nil
			}
		}
	}

	return "", fmt.Errorf("no hub url found in HTTP Link headers")
}

// Subscribe subscribes topicURL on hubURL
func Subscribe(client *http.Client, hubURL, topicURL, callbackURL, secret string, leaseSeconds int) error {
	hub, err := url.Parse(hubURL)
	if err != nil {
		return err
	}

	q := hub.Query()
	q.Add("hub.mode", "subscribe")
	q.Add("hub.callback", callbackURL)
	q.Add("hub.topic", topicURL)
	q.Add("hub.secret", secret)
	q.Add("hub.lease_seconds", fmt.Sprintf("%d", leaseSeconds))
	hub.RawQuery = ""

	res, err := client.PostForm(hub.String(), q)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	return nil
}

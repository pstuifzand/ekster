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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/pstuifzand/ekster/pkg/rss"

	"github.com/pstuifzand/ekster/pkg/linkheader"

	"github.com/pstuifzand/ekster/pkg/jsonfeed"
	"willnorris.com/go/microformats"
)

// GetHubURL finds the HubURL for topic.
//
// ctx is the whole point of the signature: both discovery requests are made
// with it, so the HEAD and the GET land under whatever asked for the hub. Made
// with client.Head/client.Get they carry no context at all, and the tracing
// transport then opens each one as a root of its own — a trace of one span that
// cannot say which feed, or which loop, caused the request.
func GetHubURL(ctx context.Context, client *http.Client, topic string) (string, error) {
	hubURL, err := parseLinkHeaders(ctx, client, topic)
	if err == nil {
		return hubURL, err
	}

	hubURL, err = parseBodyLinks(ctx, client, topic)
	if err == nil {
		return hubURL, err
	}

	return "", fmt.Errorf("no hub url found for topic %s", topic)
}

// do builds the request with ctx instead of letting the client derive one from
// context.Background().
func do(ctx context.Context, client *http.Client, method, rawurl string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawurl, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return client.Do(req)
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

func parseBodyLinks(ctx context.Context, client *http.Client, topic string) (string, error) {
	resp, err := do(ctx, client, http.MethodGet, topic, nil, "")

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
			slog.Debug("could not parse json feed", "topic", topic, "error", err)
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

func parseLinkHeaders(ctx context.Context, client *http.Client, topic string) (string, error) {
	resp, err := do(ctx, client, http.MethodHead, topic, nil, "")

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

// Subscribe subscribes topicURL on hubURL. Like GetHubURL it takes ctx so the
// POST is a child of the renewal or the follow request that asked for it.
func Subscribe(ctx context.Context, client *http.Client, hubURL, topicURL, callbackURL, secret string, leaseSeconds int) error {
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

	res, err := do(ctx, client, http.MethodPost, hub.String(), strings.NewReader(q.Encode()),
		"application/x-www-form-urlencoded")
	if err != nil {
		return err
	}

	defer res.Body.Close()

	body, _ := ioutil.ReadAll(io.LimitReader(res.Body, 1024))

	// A hub can accept the request (200/202) and still refuse the subscription
	// in the body. switchboard.p3k.io answers 200 with "Rate limited" and never
	// sends the verification request, so treating only the status code as the
	// verdict left ekster re-subscribing every minute forever.
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("hub %s rejected subscription: %s: %s", hubURL, res.Status, strings.TrimSpace(string(body)))
	}
	if strings.Contains(strings.ToLower(string(body)), "rate limit") {
		return fmt.Errorf("hub %s rate limited subscription: %s", hubURL, strings.TrimSpace(string(body)))
	}

	return nil
}

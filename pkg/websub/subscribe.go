package websub

import (
	"fmt"
	"net/http"
	"net/url"

	"linkheader"
)

// Fetcher return the response for a url
type Fetcher interface {
	Fetch(url string) (*http.Response, error)
}

// GetHubURL finds the HubURL for topic
func GetHubURL(client *http.Client, topic string) (string, error) {
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

	return "", nil
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

package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"p83.nl/go/ekster/pkg/fetch"
	"p83.nl/go/ekster/pkg/microsub"
	"willnorris.com/go/microformats"
)

func isSupportedFeedType(feedType string) bool {
	return strings.HasPrefix(feedType, "text/html") ||
		strings.HasPrefix(feedType, "application/json") ||
		strings.HasPrefix(feedType, "application/xml") ||
		strings.HasPrefix(feedType, "text/xml") ||
		strings.HasPrefix(feedType, "application/rss+xml") ||
		strings.HasPrefix(feedType, "application/atom+xml")
}

func findFeeds(cachingFetch fetch.FetcherFunc, feedURL string) ([]microsub.Feed, error) {
	resp, err := cachingFetch(feedURL)
	if err != nil {
		return nil, fmt.Errorf("while fetching %s: %w", feedURL, err)
	}
	defer resp.Body.Close()

	fetchURL, err := url.Parse(feedURL)
	md := microformats.Parse(resp.Body, fetchURL)
	if err != nil {
		return nil, fmt.Errorf("while fetching %s: %w", feedURL, err)
	}

	feedResp, err := cachingFetch(fetchURL.String())
	if err != nil {
		return nil, fmt.Errorf("in fetch of %s: %w", fetchURL, err)
	}
	defer feedResp.Body.Close()

	// TODO: Combine FeedHeader and FeedItems so we can use it here
	parsedFeed, err := fetch.FeedHeader(cachingFetch, fetchURL.String(), feedResp.Header.Get("Content-Type"), feedResp.Body)
	if err != nil {
		return nil, fmt.Errorf("in parse of %s: %w", fetchURL, err)
	}

	var feeds []microsub.Feed

	// TODO: Only include the feed if it contains some items
	feeds = append(feeds, parsedFeed)

	// Fetch alternates
	if alts, e := md.Rels["alternate"]; e {
		for _, alt := range alts {
			relURL := md.RelURLs[alt]
			log.Printf("alternate found with type %s %#v\n", relURL.Type, relURL)

			if isSupportedFeedType(relURL.Type) {
				parsedFeed, err := fetchAlternateFeed(cachingFetch, alt)

				if err != nil {
					continue
				}

				feeds = append(feeds, parsedFeed)
			}
		}
	}
	return feeds, nil
}

func fetchAlternateFeed(cachingFetch fetch.FetcherFunc, altURL string) (microsub.Feed, error) {
	feedResp, err := cachingFetch(altURL)
	if err != nil {
		return microsub.Feed{}, fmt.Errorf("fetch of %s: %v", altURL, err)
	}

	defer feedResp.Body.Close()

	parsedFeed, err := fetch.FeedHeader(cachingFetch, altURL, feedResp.Header.Get("Content-Type"), feedResp.Body)
	if err != nil {
		return microsub.Feed{}, fmt.Errorf("in parse of %s: %v", altURL, err)
	}

	return parsedFeed, nil
}

func getPossibleURLs(query string) []string {
	urls := []string{}
	if !(strings.HasPrefix(query, "https://") || strings.HasPrefix(query, "http://")) {
		secureURL := "https://" + query
		if checkURL(secureURL) {
			urls = append(urls, secureURL)
		} else {
			unsecureURL := "http://" + query
			if checkURL(unsecureURL) {
				urls = append(urls, unsecureURL)
			}
		}
	} else {
		urls = append(urls, query)
	}
	return urls
}

func checkURL(u string) bool {
	testURL, err := url.Parse(u)
	if err != nil {
		return false
	}

	resp, err := http.Head(testURL.String())

	if err != nil {
		log.Printf("Error while HEAD %s: %v\n", u, err)
		return false
	}

	defer resp.Body.Close()

	return resp.StatusCode == 200
}

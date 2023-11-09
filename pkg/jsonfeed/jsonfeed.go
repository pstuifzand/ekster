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

// Package jsonfeed parses feeds in the jsonfeed format
package jsonfeed

import (
	"encoding/json"
	"io"
)

// Attachment contains attachments for podcasts
type Attachment struct {
	URL               string `json:"url"`
	MimeType          string `json:"mime_type"`
	Title             string `json:"title,omitempty"`
	SizeInBytes       int    `json:"size_in_bytes,omitempty"`
	DurationInSeconds int    `json:"duration_in_seconds,omitempty"`
}

// Item is the main item in the feed
type Item struct {
	ID            string       `json:"id"`
	ContentText   string       `json:"content_text,omitempty"`
	ContentHTML   string       `json:"content_html,omitempty"`
	Summary       string       `json:"summary,omitempty"`
	Title         string       `json:"title,omitempty"`
	URL           string       `json:"url,omitempty"`
	Image         string       `json:"image,omitempty"`
	ExternalURL   string       `json:"external_url,omitempty"`
	DatePublished string       `json:"date_published,omitempty"`
	Author        Author       `json:"author,omitempty"`
	Tags          []string     `json:"tags,omitempty"`
	Attachments   []Attachment `json:"attachments,omitempty"`
}

// Author is the author of the Item
type Author struct {
	Name   string `json:"name,omitempty"`
	URL    string `json:"url,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// Hub contains a reference to a feed hub
type Hub struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Feed is the main object
type Feed struct {
	Version     string `json:"version"`
	Title       string `json:"title"`
	HomePageURL string `json:"home_page_url"`
	FeedURL     string `json:"feed_url"`
	NextURL     string `json:"next_url"`
	Icon        string `json:"icon"`
	Favicon     string `json:"favicon"`
	Author      Author `json:"author,omitempty"`
	Items       []Item `json:"items"`
	Hubs        []Hub  `json:"hubs"`
}

// Parse parses a jsonfeed
func Parse(body io.Reader) (Feed, error) {
	var feed Feed
	err := json.NewDecoder(body).Decode(&feed)
	return feed, err
}

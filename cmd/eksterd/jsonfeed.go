/*
   Microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

type JSONFeedAttachment struct {
	URL               string `json:"url"`
	MimeType          string `json:"mime_type"`
	Title             string `json:"title,omitempty"`
	SizeInBytes       int    `json:"size_in_bytes,omitempty"`
	DurationInSeconds int    `json:"duration_in_seconds,omitempty"`
}

type JSONFeedItem struct {
	ID            string               `json:"id"`
	ContentText   string               `json:"content_text,omitempty"`
	ContentHTML   string               `json:"content_html,omitempty"`
	Summary       string               `json:"summary,omitempty"`
	Title         string               `json:"title,omitempty"`
	URL           string               `json:"url,omitempty"`
	Image         string               `json:"image,omitempty"`
	ExternalURL   string               `json:"external_url,omitempty"`
	DatePublished string               `json:"date_published,omitempty"`
	Author        JSONFeedAuthor       `json:"author,omitempty"`
	Tags          []string             `json:"tags,omitempty"`
	Attachments   []JSONFeedAttachment `json:"attachments,omitempty"`
}

type JSONFeedAuthor struct {
	Name   string `json:"name,omitempty"`
	URL    string `json:"url,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

type JSONFeedHub struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type JSONFeed struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	NextUrl     string         `json:"next_url"`
	Icon        string         `json:"icon"`
	Favicon     string         `json:"favicon"`
	Author      JSONFeedAuthor `json:"author,omitempty"`
	Items       []JSONFeedItem `json:"items"`
	Hubs        []JSONFeedHub  `json:"hubs"`
}

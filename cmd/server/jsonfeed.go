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

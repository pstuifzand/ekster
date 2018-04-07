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
	ExternalURL   string               `json:"external_url,omitempty"`
	DatePublished string               `json:"date_published,omitempty"`
	Tags          []string             `json:"tags,omitempty"`
	Attachments   []JSONFeedAttachment `json:"attachments,omitempty"`
}

type JSONFeed struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	Items       []JSONFeedItem `json:"items"`
}

package fetch

import "net/http"

// Fetcher fetches urls
type Fetcher interface {
	Fetch(url string) (*http.Response, error)
}

// FetcherFunc is a function that fetches an url
type FetcherFunc func(url string) (*http.Response, error)

// Fetch fetches an url and returns a response or error
func (ff FetcherFunc) Fetch(url string) (*http.Response, error) {
	return ff(url)
}

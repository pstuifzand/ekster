package fetch

import "net/http"

// FetcherFunc is a function that fetches an url
type FetcherFunc func(url string) (*http.Response, error)

package fetch

import "net/http"

type Fetcher interface {
	Fetch(url string) (*http.Response, error)
}
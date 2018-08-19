package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"p83.nl/go/ekster/pkg/fetch"
)

func init() {
	f, err := os.Open(os.DevNull)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(f)
}

type fetcher struct{}

func (f fetcher) Fetch(url string) (*http.Response, error) {
	return http.Get(url)
}

func main() {
	cl := http.Client{}

	url := os.Args[1]
	resp, err := cl.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	items, err := fetch.FeedItems(fetcher{}, url, resp.Header.Get("Content-Type"), resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	json.NewEncoder(os.Stdout).Encode(items)
}

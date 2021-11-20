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

// Fetch calls http.Get
func Fetch(url string) (*http.Response, error) {
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

	items, err := fetch.FeedItems(fetch.FetcherFunc(Fetch), url, resp.Header.Get("Content-Type"), resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	json.NewEncoder(os.Stdout).Encode(items)
}

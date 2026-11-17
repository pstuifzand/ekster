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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/pstuifzand/ekster/pkg/fetch"
)

func init() {
	// The libraries log through slog, which the old log.SetOutput trick no longer
	// silences. Only errors reach stderr, so the JSON on stdout stays the output.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})))
}

// fail reports an error and exits non-zero.
func fail(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}

// Fetch calls http.Get
func Fetch(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	cl := http.Client{}
	return cl.Do(req)
}

func main() {
	cl := http.Client{}

	url := os.Args[1]
	resp, err := cl.Get(url)
	if err != nil {
		fail(err)
	}
	defer resp.Body.Close()

	items, err := fetch.FeedItems(context.Background(), fetch.FetcherFunc(Fetch), url, resp.Header.Get("Content-Type"), resp.Body)
	if err != nil {
		fail(err)
	}

	json.NewEncoder(os.Stdout).Encode(items)
}

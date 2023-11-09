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

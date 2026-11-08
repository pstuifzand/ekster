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

package auth

import (
	"strconv"
	"strings"
	"time"
)

// Auther checks of the token in the head is accepted and fills TokenResponse
type Auther interface {
	AuthTokenAccepted(header string, r *TokenResponse) bool
}

// Timestamp is issued_at, which servers send either as a unix number or as an
// RFC3339 string. Rejecting one of the two means refusing every token from
// whichever kind of server picked the other.
type Timestamp int64

// UnmarshalJSON accepts both shapes.
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*t = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*t = Timestamp(n)
		return nil
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	*t = Timestamp(ts.Unix())
	return nil
}

// TokenResponse is the information that we get back from the token endpoint of the user...
type TokenResponse struct {
	Me       string    `json:"me"`
	ClientID string    `json:"client_id"`
	Scope    string    `json:"scope"`
	IssuedAt Timestamp `json:"issued_at"`
	Nonce    int64     `json:"nonce"`
}

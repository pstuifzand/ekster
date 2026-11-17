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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The ancestor walk resolves URLs out of feed content, so "somewhere a client
// could not reach" is the whole point of the check.
func TestIsPublicAddress(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":            false,
		"127.1.2.3":            false,
		"::1":                  false,
		"0.0.0.0":              false,
		"10.1.2.3":             false,
		"172.16.0.9":           false,
		"172.31.255.254":       false,
		"192.168.68.105":       false,
		"169.254.169.254":      false, // cloud metadata
		"100.64.0.1":           false, // carrier-grade NAT
		"192.0.0.8":            false, // IETF protocol assignments
		"198.18.0.1":           false, // benchmarking
		"224.0.0.1":            false, // multicast
		"fe80::1":              false, // link-local
		"fd00::1":              false, // unique local
		"93.184.216.34":        true,
		"1.1.1.1":              true,
		"2001:4860:4860::8888": true,
		"172.32.0.1":           true, // just outside RFC 1918
	}

	for address, want := range cases {
		ip := net.ParseIP(address)
		if !assert.NotNil(t, ip, address) {
			continue
		}
		assert.Equal(t, want, isPublicAddress(ip), address)
	}
	assert.False(t, isPublicAddress(nil))
}

// A literal private address in the URL is refused before a request is built.
// A name that resolves to one is refused at dial time, by the same function.
func TestCheckPublicURL(t *testing.T) {
	rejected := []string{
		"http://127.0.0.1/status",
		"http://127.0.0.1:4003/incoming/24",
		"http://[::1]/",
		"http://169.254.169.254/latest/meta-data/",
		"http://192.168.68.105:8080/",
		"file:///etc/passwd",
		"gopher://example.com/",
		"https://user:pass@example.com/",
		"notaurl",
		"",
	}
	for _, raw := range rejected {
		assert.Error(t, checkPublicURL(raw), raw)
	}

	accepted := []string{
		"https://example.com/post/1",
		"http://example.com/",
		"https://93.184.216.34/x",
	}
	for _, raw := range accepted {
		assert.NoError(t, checkPublicURL(raw), raw)
	}
}

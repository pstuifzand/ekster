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
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/pstuifzand/ekster/pkg/fetch"
)

// A thread resolves ancestors by following in-reply-to links out of content
// the server did not choose: a feed can point an entry at any URL it likes. A
// user-chosen URL (follow, preview) is the user's business; a URL a feed chose
// for the server is not, so those fetches go through a client that will not
// connect anywhere a client of its own could not reach.

// maxThreadRedirects bounds a redirect chain. Every hop is dialled through the
// same control function, so the bound is about work, not about safety.
const maxThreadRedirects = 5

// isPublicAddress reports whether a dialled address is somewhere a public
// client could reach. Loopback, RFC 1918, link-local (cloud metadata lives at
// 169.254.169.254), unique-local IPv6, CGNAT, multicast and the unspecified
// address are all out.
func isPublicAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 is carrier-grade NAT: reachable only from inside a
		// provider's network, never from the public web.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		// 192.0.0.0/24 is IETF protocol assignments; 198.18.0.0/15 is
		// benchmarking. Neither is a website.
		if v4[0] == 192 && v4[1] == 0 && v4[2] == 0 {
			return false
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return false
		}
	}
	return true
}

// publicOnlyClient builds an http.Client that refuses non-public addresses.
//
// The check lives in the dialer's Control hook because that is the only place
// that sees the resolved address: checking the hostname loses to DNS
// rebinding and to names that resolve to loopback. Because every hop of a
// redirect is dialled through it, a public URL that 302s to 127.0.0.1 is
// refused at the second hop without needing a separate redirect check.
func publicOnlyClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("refusing to dial %q: %w", address, err)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("refusing to dial %q: not an ip address", address)
			}
			if !isPublicAddress(ip) {
				return fmt.Errorf("refusing to dial %s: not a public address", address)
			}
			return nil
		},
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: identifyingTransport{next: transport},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxThreadRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// publicFetcher is a fetch.Fetcher for URLs chosen by someone else.
func publicFetcher(timeout time.Duration) fetch.Fetcher {
	client := publicOnlyClient(timeout)
	return fetch.FetcherFunc(func(ctx context.Context, u string) (*http.Response, error) {
		if err := checkPublicURL(u); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		return client.Do(req)
	})
}

// checkPublicURL rejects the obvious cases before a request is even built: a
// literal private address in the URL, credentials, and anything that is not
// http(s). Names are decided at dial time, where the resolved address is known.
func checkPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a url: %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("not an http url: %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("url without a host: %q", raw)
	}
	if u.User != nil {
		return fmt.Errorf("url with credentials: %q", raw)
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !isPublicAddress(ip) {
		return fmt.Errorf("refusing to fetch %s: not a public address", u.Hostname())
	}
	return nil
}

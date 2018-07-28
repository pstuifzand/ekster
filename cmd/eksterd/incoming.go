/*
   Microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type incomingHandler struct {
	Backend HubBackend
}

var (
	urlRegex = regexp.MustCompile(`^/incoming/(\d+)$`)
)

func (h *incomingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	r.ParseForm()
	log.Printf("%s %s\n", r.Method, r.URL)
	log.Println(r.URL.Query())
	log.Println(r.PostForm)

	// find feed
	matches := urlRegex.FindStringSubmatch(r.URL.Path)
	feed, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		fmt.Fprint(w, err)
	}

	if r.Method == http.MethodGet {
		values := r.URL.Query()

		// check
		if leaseStr := values.Get("hub.lease_seconds"); leaseStr != "" {
			// update lease_seconds

			leaseSeconds, err := strconv.ParseInt(leaseStr, 10, 64)
			if err != nil {
				http.Error(w, fmt.Sprintf("error in hub.lease_seconds format %q: %s", leaseSeconds, err), 400)
				return
			}
			err = h.Backend.FeedSetLeaseSeconds(feed, leaseSeconds)
			if err != nil {
				http.Error(w, fmt.Sprintf("error in while setting hub.lease_seconds: %s", err), 400)
				return
			}
		}

		verify := values.Get("hub.challenge")
		fmt.Fprint(w, verify)

		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// find secret
	secret := h.Backend.GetSecret(feed)
	if secret == "" {
		log.Printf("missing secret for feed %d\n", feed)
		http.Error(w, "Unknown", 400)
		return
	}

	feedContent, err := ioutil.ReadAll(r.Body)

	// match signature
	sig := r.Header.Get("X-Hub-Signature")
	if sig != "" {
		if err := isHubSignatureValid(sig, feedContent, secret); err != nil {
			http.Error(w, fmt.Sprintf("Error in signature: %s", err), 400)
			return
		}
	}

	ct := r.Header.Get("Content-Type")
	err = h.Backend.UpdateFeed(feed, ct, bytes.NewBuffer(feedContent))
	if err != nil {
		http.Error(w, fmt.Sprintf("Unknown format of body: %s (%s)", ct, err), 400)
		return
	}

	return
}

func isHubSignatureValid(sig string, feedContent []byte, secret string) error {
	parts := strings.Split(sig, "=")

	if len(parts) != 2 {
		return fmt.Errorf("signature format is not like sha1=signature")
	}

	if parts[0] != "sha1" {
		return fmt.Errorf("signature format is not like sha1=signature")
	}

	// verification
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(feedContent)
	signature := mac.Sum(nil)

	if fmt.Sprintf("%x", signature) != parts[1] {
		return fmt.Errorf("signature does not match feed %s %s", signature, parts[1])
	}

	return nil
}

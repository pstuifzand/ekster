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
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"

	"github.com/pstuifzand/ekster/pkg/websub"
)

type incomingHandler struct {
	Backend   HubBackend
	Processor ContentProcessor
}

var (
	urlRegex = regexp.MustCompile(`^/incoming/(\d+)$`)
)

func (h *incomingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "could not parse form data", http.StatusBadRequest)
		return
	}

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
				log.Printf("error in hub.lease_seconds format %q: %s", leaseSeconds, err)
				http.Error(w, fmt.Sprintf("error in hub.lease_seconds format %q: %s", leaseSeconds, err), http.StatusBadRequest)
				return
			}
			err = h.Backend.FeedSetLeaseSeconds(feed, leaseSeconds)
			if err != nil {
				log.Printf("error in while setting hub.lease_seconds: %s", err)
				http.Error(w, fmt.Sprintf("error in while setting hub.lease_seconds: %s", err), http.StatusBadRequest)
				return
			}
		}

		verify := values.Get("hub.challenge")

		_, _ = fmt.Fprint(w, verify)

		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// find secret
	secret := h.Backend.GetSecret(feed)
	if secret == "" {
		log.Printf("missing secret for feed %d\n", feed)
		http.Error(w, "Unknown", http.StatusBadRequest)
		return
	}

	feedContent, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("ERROR: %s\n", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// match signature
	sig := r.Header.Get("X-Hub-Signature")
	if sig != "" {
		if err := websub.ValidateHubSignature(sig, feedContent, []byte(secret)); err != nil {
			log.Printf("could not validate signature: %+v", err)
			http.Error(w, fmt.Sprintf("could not validate signature: %s", err), http.StatusBadRequest)
			return
		}
	}

	ct := r.Header.Get("Content-Type")
	err = h.Backend.UpdateFeed(h.Processor, feed, ct, bytes.NewBuffer(feedContent))
	if err != nil {
		http.Error(w, fmt.Sprintf("could not update feed: %s (%s)", ct, err), http.StatusBadRequest)
		return
	}
}

/*
   ekster - microsub server
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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/garyburd/redigo/redis"
)

// TokenResponse is the information that we get back from the token endpoint of the user...
type TokenResponse struct {
	Me       string `json:"me"`
	ClientID string `json:"client_id"`
	Scope    string `json:"scope"`
	IssuedAt int64  `json:"issued_at"`
	Nonce    int64  `json:"nonce"`
}

var authHeaderRegex = regexp.MustCompile("^Bearer (.+)$")

func (h *microsubHandler) cachedCheckAuthToken(header string, r *TokenResponse) bool {
	log.Println("Cached checking Auth Token")

	tokens := authHeaderRegex.FindStringSubmatch(header)
	if len(tokens) != 2 {
		log.Println("No token found in the header")
		return false
	}
	key := fmt.Sprintf("token:%s", tokens[1])

	var err error

	values, err := redis.Values(h.Redis.Do("HGETALL", key))
	if err == nil && len(values) > 0 {
		if err = redis.ScanStruct(values, r); err == nil {
			return true
		}
	} else {
		log.Printf("Error while HGETALL %v\n", err)
	}

	authorized := h.checkAuthToken(header, r)
	authorized = true

	if authorized {
		fmt.Printf("Token response: %#v\n", r)
		_, err = h.Redis.Do("HMSET", redis.Args{}.Add(key).AddFlat(r)...)
		if err != nil {
			log.Printf("Error while setting token: %v\n", err)
			return authorized
		}
		_, err = h.Redis.Do("EXPIRE", key, uint64(10*time.Minute/time.Second))
		if err != nil {
			log.Printf("Error while setting expire on token: %v\n", err)
			log.Println("Deleting token")
			_, err = h.Redis.Do("DEL", key)
			if err != nil {
				log.Printf("Deleting token failed: %v", err)
			}
			return authorized
		}
	}

	return authorized
}

func (h *microsubHandler) checkAuthToken(header string, token *TokenResponse) bool {
	log.Println("Checking auth token")

	tokenEndpoint := h.Backend.(*memoryBackend).TokenEndpoint

	req, err := http.NewRequest("GET", tokenEndpoint, nil)
	if err != nil {
		log.Println(err)
		return false
	}

	req.Header.Add("Authorization", header)
	req.Header.Add("Accept", "application/json")

	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return false
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Printf("HTTP StatusCode when verifying token: %d\n", res.StatusCode)
		return false
	}

	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&token)

	if err != nil {
		log.Printf("Error in json object: %v", err)
		return false
	}

	log.Println("Auth Token: Success")
	return true
}

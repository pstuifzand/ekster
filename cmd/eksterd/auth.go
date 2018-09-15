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

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/auth"
)

var authHeaderRegex = regexp.MustCompile("^Bearer (.+)$")

func (b *memoryBackend) cachedCheckAuthToken(conn redis.Conn, header string, r *auth.TokenResponse) bool {
	log.Println("Cached checking Auth Token")

	tokens := authHeaderRegex.FindStringSubmatch(header)

	if len(tokens) != 2 {
		log.Println("No token found in the header")
		return false
	}

	key := fmt.Sprintf("token:%s", tokens[1])

	authorized, err := getCachedValue(conn, key, r)
	if err != nil {
		log.Println(err)
	}

	if authorized {
		return true
	}

	authorized = b.checkAuthToken(header, r)
	if authorized {
		fmt.Printf("Token response: %#v\n", r)
		setCachedTokenResponseValue(conn, key, r)
		return true
	}

	return authorized
}

// setCachedTokenResponseValue remembers the value of the auth token response in redis
func setCachedTokenResponseValue(conn redis.Conn, key string, r *auth.TokenResponse) {
	_, err := conn.Do("HMSET", redis.Args{}.Add(key).AddFlat(r)...)
	if err != nil {
		log.Printf("Error while setting token: %v\n", err)
	} else {
		_, err = conn.Do("EXPIRE", key, uint64(10*time.Minute/time.Second))
		if err != nil {
			log.Printf("Error while setting expire on token: %v\n", err)
			log.Println("Deleting token")
			_, err = conn.Do("DEL", key)
			if err != nil {
				log.Printf("Deleting token failed: %v", err)
			}
		}
	}
}

// getCachedValue gets the cached value from Redis
func getCachedValue(conn redis.Conn, key string, r *auth.TokenResponse) (bool, error) {
	values, err := redis.Values(conn.Do("HGETALL", key))
	if err == nil && len(values) > 0 {
		if err = redis.ScanStruct(values, r); err == nil {
			return true, nil
		}
	}
	return false, fmt.Errorf("error while getting value from backend: %v", err)
}

func (b *memoryBackend) checkAuthToken(header string, token *auth.TokenResponse) bool {
	log.Println("Checking auth token")

	tokenEndpoint := b.TokenEndpoint

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

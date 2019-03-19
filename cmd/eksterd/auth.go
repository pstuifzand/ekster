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
		err = setCachedTokenResponseValue(conn, key, r)
		if err != nil {
			log.Println(err)
		}
		return true
	}

	return authorized
}

func (b *memoryBackend) checkAuthToken(header string, token *auth.TokenResponse) bool {
	log.Println("Checking auth token")

	tokenEndpoint := b.TokenEndpoint

	req, err := buildValidateAuthTokenRequest(tokenEndpoint, header)
	if err != nil {
		return false
	}

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

func buildValidateAuthTokenRequest(tokenEndpoint string, header string) (*http.Request, error) {
	req, err := http.NewRequest("GET", tokenEndpoint, nil)
	req.Header.Add("Authorization", header)
	req.Header.Add("Accept", "application/json")
	return req, err
}

// setCachedTokenResponseValue remembers the value of the auth token response in redis
func setCachedTokenResponseValue(conn redis.Conn, key string, r *auth.TokenResponse) error {
	_, err := conn.Do("HMSET", redis.Args{}.Add(key).AddFlat(r)...)
	if err != nil {
		return fmt.Errorf("error while setting token: %v", err)
	}
	conn.Do("EXPIRE", key, uint64(10*time.Minute/time.Second))
	return nil
}

// getCachedValue gets the cached value from Redis
func getCachedValue(conn redis.Conn, key string, r *auth.TokenResponse) (bool, error) {
	values, err := redis.Values(conn.Do("HGETALL", key))
	if err != nil {
		return false, fmt.Errorf("error while getting value from backend: %v", err)
	}

	if len(values) > 0 {
		if err = redis.ScanStruct(values, r); err == nil {
			return true, nil
		}
	}

	return false, fmt.Errorf("no cached value available")
}

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
	"regexp"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pkg/errors"
	"github.com/pstuifzand/ekster/pkg/auth"
)

var authHeaderRegex = regexp.MustCompile("^Bearer (.+)$")

var (
	// errNoTokenInHeader is a request that carries no bearer token at all. Web
	// scanners probe the authenticated paths constantly, and each probe is a
	// missing credential rather than a failure of this server.
	errNoTokenInHeader = errors.New("could not find token in header")

	// errNoCachedValue is a miss in the token cache, which is the normal path:
	// the token is checked with its endpoint and remembered afterwards. It was
	// returned as a plain error, so the HTTP path logged it at ERROR — about
	// forty lines a day, against a handful of real failures.
	errNoCachedValue = errors.New("no cached value available")
)

func cachedCheckAuthToken(conn redis.Conn, header string, tokenEndpoint string, r *auth.TokenResponse) (bool, error) {
	tokens := authHeaderRegex.FindStringSubmatch(header)

	if len(tokens) != 2 {
		return false, errNoTokenInHeader
	}

	key := fmt.Sprintf("token:%s", tokens[1])

	authorized, err := getCachedValue(conn, key, r)
	if err != nil {
		if errors.Is(err, errNoCachedValue) {
			slog.Debug("auth token is not cached")
		} else {
			// A cache that cannot be read is a real failure: every request then
			// pays for a token check against the endpoint.
			slog.Error("could not read the cached auth token", "error", err.Error())
		}
	}

	if authorized {
		return true, nil
	}

	authorized, err = checkAuthToken(header, tokenEndpoint, r)
	if err != nil {
		return false, errors.Wrap(err, "could not check auth token")
	}

	if authorized {
		err = setCachedTokenResponseValue(conn, key, r)
		if err != nil {
			slog.Error("could not cache the token response", "error", err.Error())
		}

		return true, nil
	}

	return authorized, nil
}

// logAuthFailure records why a request was refused. A request that carries no
// token is not an error — scanners probe the authenticated paths constantly, and
// every probe wrote an ERROR line with a stack trace — while a token the
// endpoint rejects is worth one. The error is logged as a string because slog
// prints a pkg/errors value with %+v, which turns one line into fifteen.
func logAuthFailure(ctx context.Context, err error) {
	if errors.Cause(err) == errNoTokenInHeader {
		slog.DebugContext(ctx, "request carries no bearer token")
		return
	}
	slog.ErrorContext(ctx, "token not accepted", "error", err.Error())
}

func checkAuthToken(header string, tokenEndpoint string, token *auth.TokenResponse) (bool, error) {
	req, err := buildValidateAuthTokenRequest(tokenEndpoint, header)
	if err != nil {
		return false, err
	}

	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		err := res.Body.Close()
		if err != nil {
			slog.Error("could not close the token response body", "error", err)
		}
	}()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false, fmt.Errorf("got unsuccessful http status code while verifying token: %d", res.StatusCode)
	}

	contentType := res.Header.Get("content-type")
	if strings.HasPrefix(contentType, "application/json") {
		dec := json.NewDecoder(res.Body)
		err = dec.Decode(&token)
		if err != nil {
			return false, errors.Wrap(err, "could not decode json body")
		}
		return true, nil
	}

	return false, errors.Wrapf(err, "unknown content-type %q while checking auth token", contentType)
}

func buildValidateAuthTokenRequest(tokenEndpoint string, header string) (*http.Request, error) {
	req, err := http.NewRequest("GET", tokenEndpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "could not create a new request")
	}
	req.Header.Add("Authorization", header)
	req.Header.Add("Accept", "application/json")

	return req, nil
}

// setCachedTokenResponseValue remembers the value of the auth token response in redis
func setCachedTokenResponseValue(conn redis.Conn, key string, r *auth.TokenResponse) error {
	_, err := conn.Do("HMSET", redis.Args{}.Add(key).AddFlat(r)...)
	if err != nil {
		return errors.Wrap(err, "could not remember token")
	}
	_, err = conn.Do("EXPIRE", key, uint64(10*time.Minute/time.Second))
	if err != nil {
		return errors.Wrap(err, "could not set expiration for token")
	}
	return nil
}

// getCachedValue gets the cached value from Redis
func getCachedValue(conn redis.Conn, key string, r *auth.TokenResponse) (bool, error) {
	values, err := redis.Values(conn.Do("HGETALL", key))
	if err != nil {
		return false, errors.Wrap(err, "could not get value from backend")
	}

	if len(values) > 0 {
		if err = redis.ScanStruct(values, r); err == nil {
			return true, nil
		}
	}

	return false, errNoCachedValue
}

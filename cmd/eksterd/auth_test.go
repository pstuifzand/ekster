package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"

	"github.com/pstuifzand/ekster/pkg/auth"
)

// fakeRedis answers the three commands the token cache uses, and can fail the
// lookup the way a dead Redis does.
type fakeRedis struct {
	values []interface{}
	doErr  error
	cached bool
}

func (f *fakeRedis) Do(commandName string, args ...interface{}) (interface{}, error) {
	switch strings.ToUpper(commandName) {
	case "HGETALL":
		if f.doErr != nil {
			return nil, f.doErr
		}
		return f.values, nil
	case "HMSET":
		f.cached = true
		return "OK", nil
	case "EXPIRE":
		return int64(1), nil
	}
	return nil, fmt.Errorf("unexpected command %q", commandName)
}

func (f *fakeRedis) Send(string, ...interface{}) error { return nil }
func (f *fakeRedis) Flush() error                      { return nil }
func (f *fakeRedis) Receive() (interface{}, error)     { return nil, nil }
func (f *fakeRedis) Close() error                      { return nil }
func (f *fakeRedis) Err() error                        { return nil }

// tokenEndpoint accepts the test token and answers the way IndieAuth does.
func tokenEndpoint(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer abc", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"me":"https://p83.nl/","client_id":"https://app.example/","scope":"read"}`)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// A token that is not in the cache yet is the normal state of every token the
// first time it is used: the endpoint is asked and the answer is remembered.
// Logging that miss at ERROR put about forty lines a day next to the handful of
// real failures, so the line that named a genuine problem was invisible. At the
// default level it is gone entirely.
func TestUncachedTokenIsNotLogged(t *testing.T) {
	buf := logfmtOutput(t)

	conn := &fakeRedis{}
	response := &auth.TokenResponse{}

	authorized, err := cachedCheckAuthToken(conn, "Bearer abc", tokenEndpoint(t), response)
	assert.NoError(t, err)
	assert.True(t, authorized, "the endpoint accepted the token")
	assert.True(t, conn.cached, "and the answer is remembered")

	logged := buf.String()
	assert.NotContains(t, logged, "auth token is not cached")
	assert.NotContains(t, logged, "level=ERROR")
}

// The line still exists for the question "why is every request checking its
// token with the endpoint", which is what the debug level is for.
func TestUncachedTokenIsVisibleAtDebug(t *testing.T) {
	t.Setenv("EKSTER_LOG_LEVEL", "debug")
	buf := logfmtOutput(t)

	if _, err := cachedCheckAuthToken(&fakeRedis{}, "Bearer abc", tokenEndpoint(t), &auth.TokenResponse{}); !assert.NoError(t, err) {
		return
	}
	assert.Contains(t, buf.String(), `level=DEBUG msg="auth token is not cached"`)
}

// The demotion must not swallow a cache that cannot be read: that is a real
// failure, and it stays at error.
func TestUnreadableTokenCacheIsLoggedAtError(t *testing.T) {
	buf := logfmtOutput(t)

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"me":"https://p83.nl/"}`)
	}))
	defer endpoint.Close()

	conn := &fakeRedis{doErr: fmt.Errorf("connection refused")}
	response := &auth.TokenResponse{}

	_, err := cachedCheckAuthToken(conn, "Bearer abc", endpoint.URL, response)
	assert.NoError(t, err)

	logged := buf.String()
	assert.Contains(t, logged, `level=ERROR msg="could not read the cached auth token"`)
	assert.Contains(t, logged, "connection refused")
}

// A request with no token is a scanner, not a failure of this server, and a
// refusal with one is a single line: the error used to be logged as a value,
// which slog prints with %+v and turns into fifteen lines of pkg/errors stack.
func TestAuthFailuresAreLoggedByKind(t *testing.T) {
	t.Setenv("EKSTER_LOG_LEVEL", "debug")
	buf := logfmtOutput(t)

	logAuthFailure(context.Background(), errNoTokenInHeader)
	assert.Contains(t, buf.String(), `level=DEBUG msg="request carries no bearer token"`)
	assert.NotContains(t, buf.String(), "level=ERROR")

	buf.Reset()
	rejected := pkgerrors.Wrap(fmt.Errorf("got unsuccessful http status code while verifying token: 401"), "could not check auth token")
	logAuthFailure(context.Background(), rejected)

	logged := buf.String()
	assert.Contains(t, logged, `level=ERROR msg="token not accepted"`)
	assert.Contains(t, logged, "unsuccessful http status code while verifying token: 401")
	assert.Equal(t, 1, strings.Count(logged, "\n"), "one line per refusal, no stack trace")
}

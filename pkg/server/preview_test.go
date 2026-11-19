package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// previewBackend answers PreviewURL with a canned error, so the handler's status
// mapping can be tested without a fetcher or a database.
type previewBackend struct {
	*NullBackend
	err error
}

func (b *previewBackend) PreviewURL(ctx context.Context, previewURL string) (microsub.Timeline, error) {
	if b.err != nil {
		return microsub.Timeline{}, b.err
	}
	return microsub.Timeline{Items: []microsub.Item{}}, nil
}

func preview(t *testing.T, backend microsub.Microsub, method, previewURL string) *http.Response {
	t.Helper()
	handler, _ := NewMicrosubHandler(backend)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	query := url.Values{
		"action": {"preview"},
		"url":    {previewURL},
	}

	// A GET reads its parameters from the query string only; the body is not
	// parsed, so sending the form there would exercise a different branch.
	target := server.URL
	var body *strings.Reader
	if method == http.MethodGet {
		target += "?" + query.Encode()
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(query.Encode())
	}

	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s preview: %v", method, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// A URL that cannot be a feed is the caller's input, not a server failure. It
// used to answer 500, which in the reader reads as "the server is broken" for a
// value typed into the feed box — on p83-new that was every preview of an empty
// URL, three of them in eight seconds.
func TestServer_PreviewURL_NotAFeedIsBadRequest(t *testing.T) {
	backend := &previewBackend{
		NullBackend: &NullBackend{},
		err:         fmt.Errorf("%w: %q is not an http(s) url", microsub.ErrNotAFeed, "example.com"),
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		resp := preview(t, backend, method, "example.com")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want %d", method, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

// A URL with nothing in it is the same kind of answer: the request is
// incomplete, and asking the network about "" is not going to help.
func TestServer_PreviewURL_EmptyIsBadRequest(t *testing.T) {
	backend := &previewBackend{
		NullBackend: &NullBackend{},
		err:         fmt.Errorf("%w: no url to preview", microsub.ErrNotAFeed),
	}

	resp := preview(t, backend, http.MethodPost, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// Everything that is not about the URL stays a server failure: a feed host that
// times out or answers 502 is not something the person can correct by retyping.
func TestServer_PreviewURL_OtherErrorIsInternalServerError(t *testing.T) {
	backend := &previewBackend{
		NullBackend: &NullBackend{},
		err:         errors.New("error while fetching https://example.com/feed: context deadline exceeded"),
	}

	resp := preview(t, backend, http.MethodPost, "https://example.com/feed")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

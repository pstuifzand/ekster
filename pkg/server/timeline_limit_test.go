package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

type limitBackend struct {
	NullBackend
	limit                  int
	before, after, channel string
	legacy                 bool
}

func (b *limitBackend) TimelineGet(_ context.Context, before, after, channel string) (microsub.Timeline, error) {
	b.legacy = true
	return microsub.Timeline{Items: []microsub.Item{}}, nil
}
func (b *limitBackend) TimelineGetWithLimit(_ context.Context, before, after, channel string, limit int) (microsub.Timeline, error) {
	b.limit, b.before, b.after, b.channel = limit, before, after, channel
	return microsub.Timeline{Items: []microsub.Item{}}, nil
}
func TestTimelineLimitHTTPAndClient(t *testing.T) {
	b := &limitBackend{}
	h, _ := NewMicrosubHandler(b)
	s := httptest.NewServer(h)
	defer s.Close()
	u, _ := url.Parse(s.URL + "?existing=value")
	c := &client.Client{MicrosubEndpoint: u}
	ctx := context.Background()
	if _, err := c.TimelineGet(ctx, "", "", "home"); err != nil || !b.legacy {
		t.Fatalf("legacy: %v %+v", err, b)
	}
	if _, err := c.TimelineGetWithLimit(ctx, "new", "old", "home", 7); err != nil {
		t.Fatal(err)
	}
	if b.limit != 7 || b.before != "new" || b.after != "old" || b.channel != "home" {
		t.Fatalf("backend: %+v", b)
	}
	for _, raw := range []string{"0", "-1", "abc", "", "99999999999999999999999999", "1&limit=2"} {
		r, err := http.Get(s.URL + "?action=timeline&limit=" + raw)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Errorf("limit %q: %d", raw, r.StatusCode)
		}
	}
	if _, err := c.TimelineGetWithLimit(ctx, "", "", "home", 0); err == nil {
		t.Fatal("zero accepted")
	}
}

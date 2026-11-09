package websub

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSubscribe(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    bool
		wantErrSub string
	}{
		{
			name:   "accepted",
			status: http.StatusAccepted,
			body:   "",
		},
		{
			name:   "ok with empty body",
			status: http.StatusOK,
			body:   "OK",
		},
		{
			// switchboard.p3k.io answers 200 with this body and never sends the
			// verification request, so a status-only check retried forever.
			name:       "rate limited",
			status:     http.StatusOK,
			body:       "Rate limited",
			wantErr:    true,
			wantErrSub: "rate limited",
		},
		{
			name:       "rejected",
			status:     http.StatusBadRequest,
			body:       "invalid topic",
			wantErr:    true,
			wantErrSub: "invalid topic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotForm url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err == nil {
					gotForm = r.PostForm
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			err := Subscribe(srv.Client(), srv.URL, "https://example.com/feed", "https://example.com/incoming/1", "secret", 86400)

			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("error %q does not mention %q", err, tt.wantErrSub)
			}

			if gotForm.Get("hub.mode") != "subscribe" {
				t.Errorf("hub.mode = %q", gotForm.Get("hub.mode"))
			}
			if gotForm.Get("hub.topic") != "https://example.com/feed" {
				t.Errorf("hub.topic = %q", gotForm.Get("hub.topic"))
			}
			if gotForm.Get("hub.callback") != "https://example.com/incoming/1" {
				t.Errorf("hub.callback = %q", gotForm.Get("hub.callback"))
			}
		})
	}
}

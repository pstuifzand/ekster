/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 *  This program is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActionOfReadsQueryAndForm(t *testing.T) {
	cases := []struct {
		name string
		req  *http.Request
		want string
	}{
		{
			name: "query parameter",
			req:  httptest.NewRequest(http.MethodGet, "/microsub/1?action=timeline", nil),
			want: "timeline",
		},
		{
			name: "form body",
			req: httptest.NewRequest(http.MethodPost, "/microsub/1",
				strings.NewReader("action=mark-read&channel=home")),
			want: "mark-read",
		},
		{
			name: "no action",
			req:  httptest.NewRequest(http.MethodGet, "/microsub/1", nil),
			want: "",
		},
		{
			name: "unknown action",
			req:  httptest.NewRequest(http.MethodGet, "/microsub/1?action=../../etc/passwd", nil),
			want: "other",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if got := actionOf(tc.req); got != tc.want {
				t.Fatalf("actionOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// The action is client-supplied, so it must not be able to create arbitrary label
// values; the route and the channel have the same problem for different reasons.
func TestMetricsLabelTheActionNotTheChannel(t *testing.T) {
	t.Setenv(metricsEnv, "true")

	h := Handler(microsubRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), nil)

	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/microsub/1?action=timeline&channel=secret-uid", nil))

	body := scrape(t)
	want := `ekster_http_requests_total{action="timeline",method="GET",route="/microsub/",status="200"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in metrics output:\n%s", want, body)
	}
	if strings.Contains(body, "secret-uid") {
		t.Fatalf("a channel uid leaked into a label:\n%s", body)
	}
}

func TestMetricsMarkUnknownActionsAsOther(t *testing.T) {
	t.Setenv(metricsEnv, "true")

	h := Handler(microsubRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), nil)

	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/microsub/1?action=nonsense-value", nil))

	if body := scrape(t); !strings.Contains(body, `action="other"`) {
		t.Fatalf("an unknown action was not collapsed to \"other\":\n%s", body)
	}
}

// enrichment only applies to the microsub endpoint: an action parameter on any
// other route means nothing.
func TestEnrichRequestIgnoresOtherRoutes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/micropub?action=timeline&channel=home", nil)
	enrichRequest("/micropub", req)

	if got := actionOf(req); got != "timeline" {
		t.Fatalf("actionOf = %q; the guard is what must stop it reaching a span", got)
	}
}

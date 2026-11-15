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

package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scrape renders the registry the way the metrics endpoint does, so the test
// asserts what Prometheus would actually read.
func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler answered %d", rec.Code)
	}
	return rec.Body.String()
}

// Each test uses its own route label: the registry is process-wide, so a shared
// route would let one test's counts satisfy another's assertion.
func TestMetricsCountsRouteAndStatus(t *testing.T) {
	t.Setenv(metricsEnv, "true")

	h := metricsMiddleware("/test-count/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), nil)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test-count/42?uid=abc", nil))

	body := scrape(t)
	want := `ekster_http_requests_total{action="",method="GET",route="/test-count/",status="404"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in metrics output:\n%s", want, body)
	}
	// The path must not become a label: it carries channel uids and feed ids.
	if strings.Contains(body, `route="/test-count/42"`) {
		t.Fatalf("path leaked into a label:\n%s", body)
	}
	if !strings.Contains(body, `ekster_http_request_duration_seconds_count{action="",method="GET",route="/test-count/"} 1`) {
		t.Fatalf("duration histogram did not record the request:\n%s", body)
	}
}

func TestMetricsSkipsLongLivedRequests(t *testing.T) {
	t.Setenv(metricsEnv, "true")

	skip := func(r *http.Request) bool { return r.URL.Query().Get("action") == "events" }
	h := metricsMiddleware("/test-events/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), skip)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test-events/1?action=events", nil))

	if body := scrape(t); strings.Contains(body, `route="/test-events/"`) {
		t.Fatalf("the SSE stream was measured, which reports a duration equal to the connection:\n%s", body)
	}

	// A normal request on the same route is still measured.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test-events/1", nil))
	body := scrape(t)
	if !strings.Contains(body, `ekster_http_requests_total{action="",method="GET",route="/test-events/",status="200"} 1`) {
		t.Fatalf("a regular request was not counted:\n%s", body)
	}
}

func TestMetricsOffByDefault(t *testing.T) {
	t.Setenv(metricsEnv, "")

	called := false
	h := metricsMiddleware("/test-off/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}), nil)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/test-off/1", nil))

	if !called {
		t.Fatal("handler was not called")
	}
	if strings.Contains(scrape(t), `route="/test-off/"`) {
		t.Fatalf("metrics were recorded while disabled")
	}
}

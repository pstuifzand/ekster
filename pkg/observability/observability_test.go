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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = tp.Shutdown(context.Background())
	})

	return recorder
}

// Tracing stays off until an endpoint is configured: a plain `go run` must not
// try to reach a collector that is not there.
func TestEnabledNeedsAnEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_TRACES_EXPORTER", "")
	if Enabled() {
		t.Error("tracing is enabled without an endpoint")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	if !Enabled() {
		t.Error("tracing is not enabled with an endpoint")
	}

	t.Setenv("OTEL_SDK_DISABLED", "true")
	if Enabled() {
		t.Error("OTEL_SDK_DISABLED=true does not switch tracing off")
	}
}

// An incoming request has to be a span with a route name, not a path, so the
// reader's user id does not end up in the span name.
func TestHandlerNamesTheSpanAfterTheRoute(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	recorder := recordSpans(t)

	handler := Handler("/microsub/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), nil)

	req := httptest.NewRequest(http.MethodGet, "/microsub/42?channel=all", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if spans[0].Name() != "GET /microsub/" {
		t.Errorf("span name = %q, want %q", spans[0].Name(), "GET /microsub/")
	}
}

// The long-lived events connection is filtered out: its span would never end.
func TestHandlerSkipsFilteredRequests(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	recorder := recordSpans(t)

	handler := Handler("/microsub/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		func(r *http.Request) bool { return r.URL.Query().Get("action") == "events" })

	req := httptest.NewRequest(http.MethodGet, "/microsub/42?action=events", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if spans := recorder.Ended(); len(spans) != 0 {
		t.Errorf("the events connection produced a span: %v", spans)
	}
}

// Wrapping the default transport is what gives every outgoing fetch a span,
// because every http.Client without its own Transport uses it.
func TestInstrumentHTTPTracesOutgoingRequests(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	recorder := recordSpans(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	previous := http.DefaultTransport
	httpInstrumented = false
	t.Cleanup(func() {
		http.DefaultTransport = previous
		httpInstrumented = false
	})

	InstrumentHTTP()

	resp, err := (&http.Client{}).Get(target.URL + "/feed.xml")
	if err != nil {
		t.Fatalf("GET: %s", err)
	}
	resp.Body.Close()

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("the outgoing request produced no span")
	}
	if got := spans[0].Name(); !strings.HasPrefix(got, "GET ") || !strings.HasSuffix(got, "/feed.xml") {
		t.Errorf("span name = %q, want the method and the path", got)
	}
}

// The resource merge is where tracing quietly died once: resource.Default()
// carries a schema URL and merging a differently-schema'd resource is an
// error, which turned Setup into "could not start tracing" and no spans at all.
func TestTracerProviderResourceNamesTheService(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	recorder := tracetest.NewSpanRecorder()

	tp, err := newTracerProvider(context.Background(), "ekster-test", "1.2.3",
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	if err != nil {
		t.Fatalf("newTracerProvider: %s", err)
	}
	defer tp.Shutdown(context.Background())

	_, span := tp.Tracer("test").Start(context.Background(), "work")
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	attrs := map[string]string{}
	for _, kv := range spans[0].Resource().Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs["service.name"] != "ekster-test" {
		t.Errorf("service.name = %q, want ekster-test", attrs["service.name"])
	}
	if attrs["service.version"] != "1.2.3" {
		t.Errorf("service.version = %q, want 1.2.3", attrs["service.version"])
	}
}

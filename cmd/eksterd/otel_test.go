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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/pstuifzand/ekster/pkg/observability"
)

// recordSpans sends every span to an in-memory recorder for the duration of a
// test, so the shape of a trace can be asserted without a collector.
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

func spanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// Parsing a feed is the work worth seeing in a trace: the fetch, the header
// parse and the item parse have to hang together under one parent.
func TestFeedParsingProducesNestedSpans(t *testing.T) {
	recorder := recordSpans(t)

	fetcher := fakeFetcher(pages(), nil)

	_, err := ProcessSourcedItems(context.Background(), fetcher, "https://example.com/feed", "text/html", strings.NewReader(entryPage))
	if err != nil {
		t.Fatalf("ProcessSourcedItems: %s", err)
	}

	spans := recorder.Ended()
	parse := spanByName(spans, "feed.parse")
	if parse == nil {
		t.Fatalf("no feed.parse span in %v", spanNames(spans))
	}
	if len(parse.Attributes()) == 0 {
		t.Errorf("the feed.parse span carries no attributes")
	}

	for _, name := range []string{"feed.parse.header", "feed.parse.items"} {
		child := spanByName(spans, name)
		if child == nil {
			t.Fatalf("no %s span in %v", name, spanNames(spans))
		}
		if child.Parent().SpanID() != parse.SpanContext().SpanID() {
			t.Errorf("%s is not a child of feed.parse", name)
		}
		if child.SpanContext().TraceID() != parse.SpanContext().TraceID() {
			t.Errorf("%s is in another trace than feed.parse", name)
		}
	}
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, s.Name())
	}
	return names
}

// Fetch2 has to carry the caller's context. Without it the outbound request
// starts its own trace and the trace you opened is missing the fetch it was
// actually waiting on.
func TestFetch2KeepsTheOutboundRequestInTheTrace(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")

	// The provider has to be in place before InstrumentHTTP builds the
	// transport: otelhttp asks the global provider for a tracer when it is
	// constructed, and a tracer from the default (no-op) provider never
	// injects traceparent. Setup() has the same order for the same reason.
	recorder := recordSpans(t)
	// Setup() installs the propagator too; without it otelhttp has nothing to
	// inject into the outgoing headers.
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() { otel.SetTextMapPropagator(previousPropagator) })

	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	observability.InstrumentHTTP()

	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, span := observability.Span(context.Background(), "feed.refresh")
	resp, err := Fetch2(ctx, server.URL+"/feed.xml")
	if err != nil {
		t.Fatalf("Fetch2: %s", err)
	}
	resp.Body.Close()
	span.End()

	traceID := span.SpanContext().TraceID().String()
	if traceparent == "" {
		t.Fatal("no traceparent was sent with the outgoing request")
	}
	if !strings.Contains(traceparent, traceID) {
		t.Errorf("traceparent %q does not carry trace id %s", traceparent, traceID)
	}

	client := spanByName(recorder.Ended(), "GET "+strings.TrimPrefix(server.URL, "http://")+"/feed.xml")
	if client == nil {
		t.Fatalf("no client span in %v", spanNames(recorder.Ended()))
	}
	if client.SpanContext().TraceID().String() != traceID {
		t.Errorf("the client span is in another trace")
	}
}

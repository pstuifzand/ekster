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

// Package observability wires up OpenTelemetry tracing for ekster.
//
// Tracing is off until it is asked for: set OTEL_EXPORTER_OTLP_ENDPOINT and a
// tracer provider exports spans over OTLP/HTTP. With nothing set, nothing is
// created and nothing is sent, so tests, CI and a plain `go run` keep working
// without a collector.
package observability

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/pstuifzand/ekster"

// httpInstrumented remembers whether http.DefaultTransport already carries the
// otel RoundTripper, so calling InstrumentHTTP twice cannot nest two wrappers
// and double every outgoing span.
var httpInstrumented bool

// Enabled reports whether spans should be produced at all. An endpoint is the
// thing that makes tracing useful, so it is the thing that switches it on.
// OTEL_SDK_DISABLED=true and OTEL_TRACES_EXPORTER=none, both standard OpenTelemetry
// switches, turn it back off for tests and CI.
func Enabled() bool {
	if isTrue(os.Getenv("OTEL_SDK_DISABLED")) {
		return false
	}
	if strings.EqualFold(os.Getenv("OTEL_TRACES_EXPORTER"), "none") {
		return false
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// Tracer returns the tracer for ekster's own spans.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Span starts a span and is the shorthand used at the instrumented call sites.
func Span(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// Setup installs the tracer provider, the OTLP/HTTP exporter and trace context
// propagation. The returned function flushes spans that are still batched; it
// is a no-op when tracing is disabled.
//
// The exporter reads the usual OTEL_EXPORTER_OTLP_* variables itself, including
// the endpoint, headers and whether to use TLS. Only the OTLP/HTTP protocol is
// supported: that is what the Tempo on p83.nl exposes to containers.
func Setup(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	if !Enabled() {
		return func(context.Context) error { return nil }, nil
	}

	tp, err := newTracerProvider(ctx, serviceName, serviceVersion)
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Printf("opentelemetry: %v", err)
	}))

	InstrumentHTTP()

	log.Printf("tracing enabled: %s %s to %q", serviceName, serviceVersion, os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))

	return tp.Shutdown, nil
}

// newTracerProvider builds a provider that exports over OTLP/HTTP. Options are
// appended after the exporter and the resource, which is how tests attach a
// recorder instead of shipping spans off the machine.
func newTracerProvider(ctx context.Context, serviceName, serviceVersion string, opts ...sdktrace.TracerProviderOption) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	res, err := buildResource(serviceName, serviceVersion)
	if err != nil {
		return nil, err
	}

	providerOpts := append([]sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	}, opts...)

	return sdktrace.NewTracerProvider(providerOpts...), nil
}

// buildResource merges ekster's identity into the SDK default resource.
//
// NewSchemaless, not NewWithAttributes with semconv.SchemaURL: the SDK's own
// default resource carries a schema URL, and resource.Merge refuses two
// different ones ("conflicting Schema URL"), which silently disabled tracing.
// The attributes are the same; only the schema version is left out.
func buildResource(serviceName, serviceVersion string) (*resource.Resource, error) {
	return resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(serviceVersion),
	))
}

// InstrumentHTTP puts a span around every outgoing request made through
// http.DefaultTransport. Ekster builds fresh http.Client values all over the
// place and a nil Transport means the default one, so wrapping it here gives
// every fetch an outbound span without touching each call site.
func InstrumentHTTP() http.RoundTripper {
	if !Enabled() {
		return http.DefaultTransport
	}
	if httpInstrumented {
		return http.DefaultTransport
	}

	http.DefaultTransport = otelhttp.NewTransport(
		http.DefaultTransport,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Host + r.URL.Path
		}),
	)
	httpInstrumented = true
	return http.DefaultTransport
}

// Handler instruments an incoming HTTP handler. The span name is the route
// rather than the path, because the path carries user ids and feed ids and a
// span name is not a place for unbounded values. A nil skip traces everything.
func Handler(route string, h http.Handler, skip func(*http.Request) bool) http.Handler {
	if !Enabled() {
		return h
	}

	opts := []otelhttp.Option{
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + route
		}),
	}
	if skip != nil {
		opts = append(opts, otelhttp.WithFilter(func(r *http.Request) bool {
			return !skip(r)
		}))
	}
	return otelhttp.NewHandler(h, route, opts...)
}

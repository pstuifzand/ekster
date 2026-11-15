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
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics are off unless EKSTER_METRICS is set, and they are served on their own
// listener rather than the public port: the reverse proxy in front of eksterd
// forwards every path, so a /metrics route would be readable by anyone who can
// reach the Microsub endpoint.
//
// Labels are the route the handler was registered with, never the path. A path
// carries channel uids and feed ids, and as a label that is unbounded cardinality
// plus a leak of what people read.
const metricsEnv = "EKSTER_METRICS"

var (
	metricsOnce sync.Once

	registry *prometheus.Registry

	requestsTotal *prometheus.CounterVec
	requestDur    *prometheus.HistogramVec
)

func metricsEnabled() bool {
	return isTrue(os.Getenv(metricsEnv))
}

// initMetrics builds the registry once. The Go and process collectors come
// along because a slow request is often a GC pause or a growing heap, and those
// numbers are free.
func initMetrics() {
	metricsOnce.Do(func() {
		registry = prometheus.NewRegistry()
		registry.MustRegister(collectors.NewGoCollector())
		registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

		requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ekster_http_requests_total",
			Help: "HTTP requests handled, by route, method and status code.",
		}, []string{"route", "method", "status"})

		requestDur = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ekster_http_request_duration_seconds",
			Help:    "Time spent handling an HTTP request.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"route", "method"})

		registry.MustRegister(requestsTotal, requestDur)
	})
}

// MetricsHandler serves the exposition format for the ekster registry.
func MetricsHandler() http.Handler {
	initMetrics()
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// StartMetricsServer listens on addr (host:port, normally loopback) and serves
// /metrics there. It returns a no-op when EKSTER_METRICS is unset, so an
// unconfigured deployment behaves exactly as before.
func StartMetricsServer(ctx context.Context, addr string) (func(context.Context) error, error) {
	if !metricsEnabled() || addr == "" {
		return func(context.Context) error { return nil }, nil
	}

	initMetrics()

	mux := http.NewServeMux()
	mux.Handle("/metrics", MetricsHandler())
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server stopped: %v", err)
		}
	}()
	log.Printf("metrics: serving /metrics on %s", ln.Addr())

	return srv.Shutdown, nil
}

// metricsMiddleware counts one request and its duration. It only measures work
// that finishes: a skipped request (the SSE stream, which never returns while
// the client is connected) is passed through untouched, because a duration that
// is the length of a connection says nothing about the server.
func metricsMiddleware(route string, h http.Handler, skip func(*http.Request) bool) http.Handler {
	if !metricsEnabled() {
		return h
	}
	initMetrics()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip != nil && skip(r) {
			h.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		requestsTotal.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		requestDur.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder remembers the status code the handler wrote, and keeps
// http.Flusher working so streaming responses are not held back by the wrapper.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

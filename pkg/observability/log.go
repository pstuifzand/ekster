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
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// LogHandler wraps an slog handler so a line logged with a context that carries
// an active span also carries trace_id and span_id.
//
// The names are not free choices: trace_id (32 lowercase hex) and span_id (16)
// are what VictoriaLogs, Loki and Grafana use to join a log line to the trace it
// belongs to, so a failing request can be read as one story instead of two
// searches. An id that is all zeros - slog.Info with no context, or a context
// without a span - is left out rather than written as 000…0, because a zero id
// matches nothing and would make every startup line look correlatable.
//
// Only context-carrying calls get the fields. Use slog.InfoContext(ctx, …) and
// friends wherever a context is in scope; a plain slog.Info is right only for
// lines that belong to no request at all (startup, background loops).
func LogHandler(next slog.Handler) slog.Handler {
	return traceHandler{Handler: next}
}

type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs and WithGroup must unwrap the same way, or a logger built with
// slog.Default().With(…) drops the ids silently.
func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{Handler: h.Handler.WithGroup(name)}
}

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
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// spanContext is a fixed, valid span context so the test asserts the exact ids
// that end up in the line instead of "some id is present".
func spanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		},
		SpanID:     trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
		TraceFlags: trace.FlagsSampled,
	})
}

func logfmtBuffer() (*bytes.Buffer, *slog.Logger) {
	buf := &bytes.Buffer{}
	return buf, slog.New(LogHandler(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

// The whole point: a line logged with a request's context carries the ids that
// join it to that request's trace in VictoriaLogs.
func TestLogHandlerAddsTraceAndSpanIDsFromTheContext(t *testing.T) {
	buf, logger := logfmtBuffer()
	sc := spanContext()
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.InfoContext(ctx, "insert item", "feed_id", 24)

	line := buf.String()
	for _, want := range []string{
		"trace_id=" + sc.TraceID().String(),
		"span_id=" + sc.SpanID().String(),
		"feed_id=24",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q is missing %q", line, want)
		}
	}
}

// A logger built from With must keep the ids, otherwise every call site that
// pre-bakes fields silently loses them.
func TestLogHandlerKeepsIDsThroughWith(t *testing.T) {
	buf, logger := logfmtBuffer()
	sc := spanContext()
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.With("component", "feeds").InfoContext(ctx, "refreshing")

	if line := buf.String(); !strings.Contains(line, "trace_id="+sc.TraceID().String()) {
		t.Errorf("line %q lost the trace id through With", line)
	}
}

// Lines that belong to no request must not carry a zero id: 000…0 is a value
// that matches nothing and makes every startup line look correlatable.
func TestLogHandlerLeavesUnrelatedLinesAlone(t *testing.T) {
	buf, logger := logfmtBuffer()

	logger.Info("eksterd starting", "version", "build-abc1234")
	logger.InfoContext(context.Background(), "no span here")

	line := buf.String()
	for _, unwanted := range []string{"trace_id=", "span_id="} {
		if strings.Contains(line, unwanted) {
			t.Errorf("line %q carries %q without a span", line, unwanted)
		}
	}
}

// A record that already carries fields keeps them when the ids are appended,
// i.e. AddAttrs on the record copy must not drop what the caller logged.
func TestLogHandlerAppendsAfterCallerFields(t *testing.T) {
	buf, logger := logfmtBuffer()
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext())

	logger.WarnContext(ctx, "subscription rejected", "feed_id", 25, "hub", "https://hub.example.com/")

	line := buf.String()
	feedPos := strings.Index(line, "feed_id=25")
	tracePos := strings.Index(line, "trace_id=")
	if feedPos < 0 || tracePos < 0 {
		t.Fatalf("line %q is missing a field", line)
	}
	if tracePos < feedPos {
		t.Errorf("trace id was written before the caller's fields: %q", line)
	}
}

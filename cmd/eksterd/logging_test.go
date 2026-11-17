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

package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// logfmtOutput installs a buffer-backed logfmt handler for the duration of a test
// and returns the buffer. Everything the process logs ends up there.
func logfmtOutput(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	previous := slog.Default()
	previousWriter := log.Writer()
	previousFlags := log.Flags()

	// The production handler, not a copy of it: a test that builds its own
	// TextHandler cannot notice the trace wrapper going missing.
	slog.SetDefault(slog.New(newLogHandler(buf)))
	log.SetFlags(0)
	log.SetOutput(slogWriter{})

	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	return buf
}

// The bridge exists so an unconverted log.Printf call still produces a line that
// unpack_logfmt can read. Without it half the stream would be plain text.
func TestStdlibLogIsWrittenAsLogfmt(t *testing.T) {
	buf := logfmtOutput(t)

	log.Println("Feed update process started")

	line := strings.TrimSpace(buf.String())
	if !strings.Contains(line, "level=INFO") {
		t.Fatalf("no level in %q", line)
	}
	if !strings.Contains(line, `msg="Feed update process started"`) {
		t.Fatalf("no msg field in %q", line)
	}
}

func TestStructuredFieldsAppearAsKeyValuePairs(t *testing.T) {
	buf := logfmtOutput(t)

	slog.Warn("websub: subscription rejected",
		"feed_id", 25,
		"topic", "https://example.com/feed",
		"hub", "https://hub.example.com/",
		"error", errorString(errors.New("422 Unprocessable Entity")))

	line := buf.String()
	for _, want := range []string{
		"level=WARN",
		"feed_id=25",
		"topic=https://example.com/feed",
		"hub=https://hub.example.com/",
		`error="422 Unprocessable Entity"`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

// A line logged with a request's context has to carry trace_id and span_id,
// because that is how VictoriaLogs joins it to the trace of the request.
func TestLogLinesCarryTheRequestTraceIDs(t *testing.T) {
	buf := logfmtOutput(t)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
		SpanID:     trace.SpanID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	slog.InfoContext(ctx, "hub delivery", "subscription", "/incoming/24")

	line := buf.String()
	for _, want := range []string{"trace_id=" + sc.TraceID().String(), "span_id=" + sc.SpanID().String()} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

func TestLogLevelFromEnv(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"WARN":    slog.LevelWarn,
		"error":   slog.LevelError,
		"warning": slog.LevelWarn,
		"verbose": slog.LevelInfo,
	}

	for value, want := range cases {
		t.Setenv("EKSTER_LOG_LEVEL", value)
		if got := logLevel(); got != want {
			t.Fatalf("logLevel(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestFeedAttrsStayKeyValuePairs(t *testing.T) {
	attrs := feedAttrs(37, "https://example.com/")
	if len(attrs)%2 != 0 {
		t.Fatalf("odd number of attributes: %v", attrs)
	}
	if attrs[0] != "feed_id" || attrs[2] != "url" {
		t.Fatalf("unexpected attribute keys: %v", attrs)
	}
}

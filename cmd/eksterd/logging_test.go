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
	"errors"
	"log"
	"log/slog"
	"strings"
	"testing"
)

// logfmtOutput installs a buffer-backed logfmt handler for the duration of a test
// and returns the buffer. Everything the process logs ends up there.
func logfmtOutput(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	previous := slog.Default()
	previousWriter := log.Writer()
	previousFlags := log.Flags()

	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
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

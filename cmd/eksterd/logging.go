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
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
)

// setupLogging installs a logfmt logger for the whole process.
//
// The point is VictoriaLogs: it stores the journal body as _msg, and
// `_msg | unpack_logfmt` turns every key=value pair into a field. A refresh that
// fails for one feed then becomes `feed_id:123` in a query instead of a substring
// match on a sentence that also matches three other lines.
//
// Package log keeps working. Its output is routed through slog, so an
// unconverted log.Printf call still emits one logfmt line instead of a second
// format in the same stream - mixed formats are worse than no fields, because
// unpack_logfmt would silently produce nothing for half the lines.
func setupLogging() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(),
	})
	slog.SetDefault(slog.New(handler))

	log.SetFlags(0)
	log.SetOutput(slogWriter{})
}

// logLevel reads EKSTER_LOG_LEVEL, defaulting to info.
func logLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EKSTER_LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// slogWriter adapts package log to slog. Each Write is one log line, already
// formatted by the caller.
type slogWriter struct{}

func (slogWriter) Write(p []byte) (int, error) {
	message := strings.TrimRight(string(p), "\n")
	if message != "" {
		slog.Info(message)
	}
	return len(p), nil
}

// feedAttrs is the set of fields every feed-related line should carry, so a
// query can group failures by feed instead of matching text.
func feedAttrs(feedID int64, url string) []any {
	return []any{"feed_id", feedID, "url", url}
}

// errorString keeps the error as a field without making it the message: logfmt
// needs the message first and the fields after it.
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}

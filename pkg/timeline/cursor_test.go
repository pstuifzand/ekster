package timeline

import (
	"testing"
	"time"
)

// A bulk import gives hundreds of items the same published_at. If the cursor carries
// only the timestamp, the next page asks for published_at < ts and drops every remaining
// row at that instant: the entries exist, the channel counts them unread, and no client
// can ever page to them.
func TestItemCursorRoundTrip(t *testing.T) {
	original := "2026-11-09T19:17:19Z|16643597"

	publishedAt, id, hasID, err := parseItemCursor(original)
	if err != nil {
		t.Fatalf("parseItemCursor(%q): %v", original, err)
	}
	if !hasID {
		t.Fatal("cursor lost its id tie-breaker")
	}
	if id != 16643597 {
		t.Errorf("id = %d, want 16643597", id)
	}
	if got := publishedAt.UTC().Format(time.RFC3339); got != "2026-11-09T19:17:19Z" {
		t.Errorf("published_at = %s", got)
	}
	if got := makeItemCursor(publishedAt.UTC().Format(time.RFC3339), id); got != original {
		t.Errorf("round trip = %q, want %q", got, original)
	}
}

// Clients holding a cursor from before the tie-breaker must keep working.
func TestItemCursorAcceptsTimestampOnly(t *testing.T) {
	publishedAt, id, hasID, err := parseItemCursor("2026-11-09T19:17:19Z")
	if err != nil {
		t.Fatalf("timestamp-only cursor rejected: %v", err)
	}
	if hasID {
		t.Error("hasID should be false for a timestamp-only cursor")
	}
	if id != 0 {
		t.Errorf("id = %d, want 0", id)
	}
	if got := publishedAt.UTC().Format(time.RFC3339); got != "2026-11-09T19:17:19Z" {
		t.Errorf("published_at = %s", got)
	}
}

// Postgres prints a timestamptz as "2006-01-02 15:04:05+00", and the first version of the
// tie-breaker put that text straight into the cursor it then refused to parse.
func TestItemCursorAcceptsPostgresText(t *testing.T) {
	parsed, id, hasID, err := parseItemCursor("2026-11-09 19:17:19+00|16643597")
	if err != nil {
		t.Fatalf("postgres-style cursor rejected: %v", err)
	}
	if !hasID || id != 16643597 {
		t.Errorf("id = %d, hasID = %v", id, hasID)
	}
	if got := parsed.UTC().Format(time.RFC3339); got != "2026-11-09T19:17:19Z" {
		t.Errorf("published_at = %s", got)
	}
}

func TestItemCursorRejectsGarbage(t *testing.T) {
	if _, _, _, err := parseItemCursor("2026-11-09T19:17:19Z|not-a-number"); err == nil {
		t.Fatal("expected an error for a non-numeric id")
	}
	if _, _, _, err := parseItemCursor("yesterday|1"); err == nil {
		t.Fatal("expected an error for an unparseable timestamp")
	}
}
